package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var currentGraphRevision uint64 = 0
var revisionMu sync.Mutex

func getObjectMeta(obj interface{}) metav1.ObjectMeta {
	switch o := obj.(type) {
	case *corev1.Pod:
		return o.ObjectMeta
	case *appsv1.ReplicaSet:
		return o.ObjectMeta
	case *appsv1.Deployment:
		return o.ObjectMeta
	case *corev1.Node:
		return o.ObjectMeta
	case *corev1.Service:
		return o.ObjectMeta
	case *corev1.ConfigMap:
		return o.ObjectMeta
	case cache.DeletedFinalStateUnknown:
		if o.Obj != nil {
			switch o2 := o.Obj.(type) {
			case *corev1.Pod:
				return o2.ObjectMeta
			case *appsv1.ReplicaSet:
				return o2.ObjectMeta
			case *appsv1.Deployment:
				return o2.ObjectMeta
			case *corev1.Node:
				return o2.ObjectMeta
			case *corev1.Service:
				return o2.ObjectMeta
			case *corev1.ConfigMap:
				return o2.ObjectMeta
			default:
				log.Printf("Unknown tombstone object type: %T", o.Obj)
				return metav1.ObjectMeta{}
			}
		}
		log.Printf("Tombstone object is nil")
		return metav1.ObjectMeta{}
	default:
		log.Printf("Unknown object type: %T", obj)
		return metav1.ObjectMeta{}
	}
}

func main() {
	log.Println("Starting Satellite...")
	cfg, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	factory := informers.NewSharedInformerFactory(client, 0)

	resourceCache := NewResourceCache()

	podInf := factory.Core().V1().Pods().Informer()
	podInf.AddEventHandler(resourceCache.AddEventHandler("Pod"))

	rsInf := factory.Apps().V1().ReplicaSets().Informer()
	rsInf.AddEventHandler(resourceCache.AddEventHandler("ReplicaSet"))

	deployInf := factory.Apps().V1().Deployments().Informer()
	deployInf.AddEventHandler(resourceCache.AddEventHandler("Deployment"))

	nodeInf := factory.Core().V1().Nodes().Informer()
	nodeInf.AddEventHandler(resourceCache.AddEventHandler("Node"))

	svcInf := factory.Core().V1().Services().Informer()
	svcInf.AddEventHandler(resourceCache.AddEventHandler("Service"))

	cmInf := factory.Core().V1().ConfigMaps().Informer()
	cmInf.AddEventHandler(resourceCache.AddEventHandler("ConfigMap"))

	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		close(stopCh)
	}()

	factory.Start(stopCh)

	log.Println("Waiting for initial cache sync...")
	if !cache.WaitForCacheSync(stopCh,
		podInf.HasSynced,
		rsInf.HasSynced,
		deployInf.HasSynced,
		nodeInf.HasSynced,
		svcInf.HasSynced,
		cmInf.HasSynced) {
		log.Fatalln("Failed to sync caches")
	}
	log.Println("Caches synced.")

	// --- Graph build loop (event-driven) ---
	log.Println("Starting graph build loop...")

Loop:
	for {
		select {
		case <-resourceCache.Changed(): // wait for cache change signal
			revisionMu.Lock()
			currentGraphRevision++
			graphRevision := currentGraphRevision
			revisionMu.Unlock()

			log.Printf("Cache changed: Building graph revision %d\n", graphRevision)
			graph := BuildGraph(resourceCache, graphRevision)

			// emit graph
			// TODO: Get outputDir from flag
			outputDir := "./data"
			if err := EmitGraph(graph, outputDir); err != nil {
				log.Printf("Error emitting graph revision %d: %v\n", graphRevision, err)
			}

		case <-stopCh:
			log.Println("Received stop signal, exiting build loop.")
			break Loop
		}
	}

	log.Println("Shutdown complete.")
}

package main

import (
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"

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
	// --- CLI Flags ---
	outputDir := flag.String("output-dir", "./data", "Directory to write graph JSON files.")
	logLevelStr := flag.String("log-level", "info", "Log level (debug, info, warn, error, fatal, panic).")
	flag.Parse()

	// --- Logger Setup ---
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)
	level, err := log.ParseLevel(*logLevelStr)
	if err != nil {
		log.Warnf("Invalid log level '%s', defaulting to 'info': %v", *logLevelStr, err)
		level = log.InfoLevel
	}
	log.SetLevel(level)
	log.Infof("Log level set to: %s", level.String())
	log.Info("Starting Satellite...")

	// --- K8s Client Setup ---
	cfg, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	// --- Informers & Cache Setup ---
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

	// --- Signal Handling & Start ---
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("Shutting down...")
		close(stopCh)
	}()

	factory.Start(stopCh)

	// --- Wait for Sync ---
	log.Info("Waiting for initial cache sync...")
	if !cache.WaitForCacheSync(stopCh,
		podInf.HasSynced,
		rsInf.HasSynced,
		deployInf.HasSynced,
		nodeInf.HasSynced,
		svcInf.HasSynced,
		cmInf.HasSynced) {
		log.Fatal("Failed to sync caches")
	}
	log.Info("Caches synced.")

	// --- Graph Build Loop ---
	log.Info("Starting graph build loop...")
Loop:
	for {
		select {
		case <-resourceCache.Changed():
			revisionMu.Lock()
			currentGraphRevision++
			graphRevision := currentGraphRevision
			revisionMu.Unlock()

			log.Debugf("Cache changed: Building graph revision %d", graphRevision)
			graph := BuildGraph(resourceCache, graphRevision)

			if err := EmitGraph(graph, *outputDir); err != nil {
				log.Errorf("Error emitting graph revision %d: %v", graphRevision, err)
			}

		case <-stopCh:
			log.Info("Received stop signal, exiting build loop.")
			break Loop
		}
	}

	log.Info("Shutdown complete.")
}

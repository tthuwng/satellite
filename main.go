package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"

    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/client-go/informers"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/cache"
    "k8s.io/client-go/tools/clientcmd"
)

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
            case *corev1.Pod: return o2.ObjectMeta
            case *appsv1.ReplicaSet: return o2.ObjectMeta
            case *appsv1.Deployment: return o2.ObjectMeta
            case *corev1.Node: return o2.ObjectMeta
            case *corev1.Service: return o2.ObjectMeta
            case *corev1.ConfigMap: return o2.ObjectMeta
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

func newEventHandler(resourceType string, ch chan<- runtime.Object) cache.ResourceEventHandlerFuncs {
    return cache.ResourceEventHandlerFuncs{
        AddFunc: func(obj interface{}) {
            meta := getObjectMeta(obj)
            log.Printf("ADD %s: %s/%s\n", resourceType, meta.Namespace, meta.Name)
            ch <- obj.(runtime.Object)
        },
        UpdateFunc: func(oldObj, newObj interface{}) {
            meta := getObjectMeta(newObj)
            log.Printf("UPDATE %s: %s/%s\n", resourceType, meta.Namespace, meta.Name)
            ch <- newObj.(runtime.Object)
        },
        DeleteFunc: func(obj interface{}) {
            robj, ok := obj.(runtime.Object)
            if !ok {
                tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
                if !ok {
                    log.Printf("Error decoding deleted object, invalid type: %T\n", obj)
                    return
                }
                robj, ok = tombstone.Obj.(runtime.Object)
                if !ok {
                    log.Printf("Error decoding deleted object tombstone, invalid type: %T\n", tombstone.Obj)
                    return
                }
            }
            meta := getObjectMeta(robj)
            log.Printf("DELETE %s: %s/%s\n", resourceType, meta.Namespace, meta.Name)
            ch <- robj
        },
    }
}

func main() {
    cfg, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
    if err != nil { 
        log.Fatalf("Error building kubeconfig: %s", err.Error())
     }

    client, err := kubernetes.NewForConfig(cfg)
    if err != nil {
        log.Fatalf("Error building kubernetes clientset: %s", err.Error())
    }

    factory := informers.NewSharedInformerFactory(client, 0)

    updatesCh := make(chan runtime.Object, 100)

    podInf := factory.Core().V1().Pods().Informer()
    podInf.AddEventHandler(newEventHandler("Pod", updatesCh))

    rsInf := factory.Apps().V1().ReplicaSets().Informer()
    rsInf.AddEventHandler(newEventHandler("ReplicaSet", updatesCh))

    deployInf := factory.Apps().V1().Deployments().Informer()
    deployInf.AddEventHandler(newEventHandler("Deployment", updatesCh))

    nodeInf := factory.Core().V1().Nodes().Informer()
    nodeInf.AddEventHandler(newEventHandler("Node", updatesCh))

    svcInf := factory.Core().V1().Services().Informer()
    svcInf.AddEventHandler(newEventHandler("Service", updatesCh))

    cmInf := factory.Core().V1().ConfigMaps().Informer()
    cmInf.AddEventHandler(newEventHandler("ConfigMap", updatesCh))

    go func() {
        for obj := range updatesCh {
            log.Printf("Processing update for: %s\n", getObjectMeta(obj).Name)
        }
    }()

    stopCh := make(chan struct{})
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigCh
        log.Println("Shutting down...")
        close(stopCh)
    }()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    factory.Start(ctx.Done())

    log.Println("Waiting for initial cache sync...")
    if !cache.WaitForCacheSync(ctx.Done(),
        podInf.HasSynced,
        rsInf.HasSynced,
        deployInf.HasSynced,
        nodeInf.HasSynced,
        svcInf.HasSynced,
        cmInf.HasSynced) {
        log.Fatalln("Failed to sync caches")
    }
    log.Println("Caches synced.")

    <-stopCh
    log.Println("Shutdown complete.")
}

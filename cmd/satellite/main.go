package main

import (
	"flag"
	"os"
	"os/signal"
	"satellite/internal/cache"
	"satellite/internal/emitter"
	"satellite/internal/graph"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	cachepkg "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var currentGraphRevision uint64 = 0
var revisionMu sync.Mutex

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
	resourceCache := cache.NewResourceCache()
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
	shutdownCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Infof("Received signal: %s. Shutting down...", sig)
		close(shutdownCh)
		close(stopCh)
	}()

	factory.Start(stopCh)

	// --- Wait for Sync ---
	log.Info("Waiting for initial cache sync...")
	if !cachepkg.WaitForCacheSync(stopCh,
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
			drain(resourceCache.Changed())
			revisionMu.Lock()
			currentGraphRevision++
			graphRevision := currentGraphRevision
			revisionMu.Unlock()

			log.Debugf("Cache changed: Building graph revision %d", graphRevision)
			graphData := graph.BuildGraph(resourceCache, graphRevision)

			if err := emitter.EmitGraph(graphData, *outputDir); err != nil {
				log.Errorf("Error emitting graph revision %d: %v", graphRevision, err)
			}

		case <-shutdownCh:
			log.Info("Shutdown signal received, exiting build loop for final emit.")
			break Loop
		}
	}

	log.Info("Performing final graph build and emit...")
	revisionMu.Lock()
	currentGraphRevision++
	finalGraphRevision := currentGraphRevision
	revisionMu.Unlock()

	finalGraphData := graph.BuildGraph(resourceCache, finalGraphRevision)
	if err := emitter.EmitGraph(finalGraphData, *outputDir); err != nil {
		log.Errorf("Error emitting final graph revision %d: %v", finalGraphRevision, err)
	}

	log.Info("Shutdown complete.")
}
func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

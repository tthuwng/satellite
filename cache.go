package main

import (
	"log"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // Removed unused import
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

type EntityKey struct {
	Kind      string // e.g. "Pod", "ReplicaSet", "Deployment", "Node", "Service", "ConfigMap"
	Namespace string
	Name      string
}

// holds the state of observed Kubernetes resources.
// safe for concurrent use.
type ResourceCache struct {
	store map[EntityKey]runtime.Object
	mu    sync.RWMutex
}

// creates a new empty cache.
func NewResourceCache() *ResourceCache {
	return &ResourceCache{
		store: make(map[EntityKey]runtime.Object),
	}
}

// extracts the EntityKey from a Kubernetes object.
func getKey(obj runtime.Object) (EntityKey, bool) {
	meta := getObjectMeta(obj)
	gvk := obj.GetObjectKind().GroupVersionKind()
	kind := gvk.Kind
	if kind == "" {
		// GVK might not be set on objects from the fake client in tests,
		// or sometimes from the API server directly. Infer kind from type.
		kind = getKindFromType(obj)
		if kind == "" {
			log.Printf("Warn: Could not determine Kind for object %s/%s", meta.Namespace, meta.Name)
			return EntityKey{}, false
		}
	}

	key := EntityKey{
		Kind:      kind,
		Namespace: meta.Namespace, // empty for non-namespaced resources like Node
		Name:      meta.Name,
	}
	return key, true
}

// infers the Kind string from the object's Go type.
func getKindFromType(obj runtime.Object) string {
	switch obj.(type) {
	case *corev1.Pod:
		return "Pod"
	case *appsv1.ReplicaSet:
		return "ReplicaSet"
	case *appsv1.Deployment:
		return "Deployment"
	case *corev1.Node:
		return "Node"
	case *corev1.Service:
		return "Service"
	case *corev1.ConfigMap:
		return "ConfigMap"
	default:
		log.Printf("Warn: Unknown type in getKindFromType: %T\n", obj)
		return ""
	}
}

// adds or updates an object in the cache.
func (c *ResourceCache) Upsert(obj runtime.Object) {
	key, ok := getKey(obj)
	if !ok {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("Cache Upsert: %s %s/%s\n", key.Kind, key.Namespace, key.Name)
	c.store[key] = obj
}

// deletes an object from the cache.
// handles DeletedFinalStateUnknown (tombstone) objects.
func (c *ResourceCache) Delete(obj interface{}) { // accepts interface{} to handle raw tombstones
	var robj runtime.Object

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		robj, ok = tombstone.Obj.(runtime.Object)
		if !ok {
			log.Printf("Error: Tombstone contained non-runtime.Object: %T", tombstone.Obj)
			return
		}
		log.Printf("Cache Delete (from tombstone): %s\n", tombstone.Key)
	} else {
		robj, ok = obj.(runtime.Object)
		if !ok {
			log.Printf("Error: Delete event received non-runtime.Object and non-tombstone: %T", obj)
			return
		}
	}

	key, ok := getKey(robj)
	if !ok {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("Cache Delete: %s %s/%s\n", key.Kind, key.Namespace, key.Name)
	delete(c.store, key)
}

// retrieves an object by key.
// returns the object and true if found, nil and false otherwise.
func (c *ResourceCache) Get(key EntityKey) (runtime.Object, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	obj, found := c.store[key]
	return obj, found
}

// returns a snapshot of all objects currently in the cache.
func (c *ResourceCache) List() []runtime.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()

	list := make([]runtime.Object, 0, len(c.store))
	for _, obj := range c.store {
		list = append(list, obj)
	}
	return list
}

// generates cache-updating event handlers.
func (c *ResourceCache) AddEventHandler(resourceType string) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			log.Printf("ADD %s: %s/%s\n", resourceType, getObjectMeta(obj).Namespace, getObjectMeta(obj).Name)
			c.Upsert(obj.(runtime.Object))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			meta := getObjectMeta(newObj)
			log.Printf("UPDATE %s: %s/%s\n", resourceType, meta.Namespace, meta.Name)
			c.Upsert(newObj.(runtime.Object))
		},
		DeleteFunc: func(obj interface{}) {
			meta := getObjectMeta(obj)
			log.Printf("DELETE %s: %s/%s\n", resourceType, meta.Namespace, meta.Name)
			c.Delete(obj)
		},
	}
}

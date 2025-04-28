package main

import (
	"sync"

	log "github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	store     map[EntityKey]runtime.Object
	mu        sync.RWMutex
	changedCh chan struct{}
}

// creates a new empty cache.
func NewResourceCache() *ResourceCache {
	return &ResourceCache{
		store:     make(map[EntityKey]runtime.Object),
		changedCh: make(chan struct{}, 1), // enough to signal change
	}
}

func (c *ResourceCache) Changed() <-chan struct{} {
	return c.changedCh
}

// extracts the EntityKey from a Kubernetes object.
func getKey(obj runtime.Object) (EntityKey, bool) {
	meta := getObjectMeta(obj)
	gvk := obj.GetObjectKind().GroupVersionKind()
	kind := gvk.Kind
	if kind == "" {
		kind = getKindFromType(obj)
		if kind == "" {
			log.Warnf("Could not determine Kind for object %s/%s", meta.Namespace, meta.Name)
			return EntityKey{}, false
		}
	}

	key := EntityKey{
		Kind:      kind,
		Namespace: meta.Namespace,
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
		log.Warnf("Unknown type in getKindFromType: %T", obj)
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
	log.Debugf("Cache Upsert: %s %s/%s", key.Kind, key.Namespace, key.Name)
	c.store[key] = obj
	c.mu.Unlock()
	c.signalChange()
}

// deletes an object from the cache.
// handles DeletedFinalStateUnknown (tombstone) objects.
func (c *ResourceCache) Delete(obj interface{}) { // accepts interface{} to handle raw tombstones
	var robj runtime.Object

	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		robj, ok = tombstone.Obj.(runtime.Object)
		if !ok {
			log.Errorf("Tombstone contained non-runtime.Object: %T", tombstone.Obj)
			return
		}
	} else {
		robj, ok = obj.(runtime.Object)
		if !ok {
			log.Errorf("Delete event received non-runtime.Object and non-tombstone: %T", obj)
			return
		}
	}

	key, ok := getKey(robj)
	if !ok {
		return
	}

	c.mu.Lock()
	_, exists := c.store[key]
	if exists {
		log.Debugf("Cache Delete: %s %s/%s", key.Kind, key.Namespace, key.Name)
		delete(c.store, key)
		c.mu.Unlock()
		c.signalChange()
	} else {
		c.mu.Unlock()
	}
}

// sends a non-blocking signal to changedCh.
func (c *ResourceCache) signalChange() {
	select {
	case c.changedCh <- struct{}{}:
	default:
	}
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
			meta := getObjectMeta(obj)
			log.Debugf("ADD %s: %s/%s", resourceType, meta.Namespace, meta.Name)
			c.Upsert(obj.(runtime.Object))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			meta := getObjectMeta(newObj)
			log.Debugf("UPDATE %s: %s/%s", resourceType, meta.Namespace, meta.Name)
			c.Upsert(newObj.(runtime.Object))
		},
		DeleteFunc: func(obj interface{}) {
			meta := getObjectMeta(obj)
			log.Debugf("DELETE %s: %s/%s", resourceType, meta.Namespace, meta.Name)
			c.Delete(obj)
		},
	}
}

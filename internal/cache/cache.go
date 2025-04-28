package cache

import (
	"sync"

	"satellite/internal/k8s"
	"satellite/internal/types"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	cache "k8s.io/client-go/tools/cache"
)

// ResourceCache holds the state of observed Kubernetes resources.
type ResourceCache struct {
	store     map[types.EntityKey]runtime.Object
	mu        sync.RWMutex
	changedCh chan struct{}
}

// creates a new empty cache.
func NewResourceCache() *ResourceCache {
	return &ResourceCache{
		store:     make(map[types.EntityKey]runtime.Object),
		changedCh: make(chan struct{}, 1), // enough to signal change
	}
}

// returns a channel that signals when the cache content has changed.
func (c *ResourceCache) Changed() <-chan struct{} {
	return c.changedCh
}

// Upsert adds or updates an object in the cache.
func (c *ResourceCache) Upsert(obj runtime.Object) {
	key, ok := k8s.GetKey(obj) // Use k8s.GetKey
	if !ok {
		return
	}

	c.mu.Lock()
	log.Debugf("Cache Upsert: %s %s/%s", key.Kind, key.Namespace, key.Name) // Debug level
	c.store[key] = obj
	c.mu.Unlock()

	c.signalChange()
}

// Delete removes an object from the cache.
func (c *ResourceCache) Delete(obj interface{}) {
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

	key, ok := k8s.GetKey(robj)
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

// Get retrieves an object by key.
func (c *ResourceCache) Get(key types.EntityKey) (runtime.Object, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	obj, found := c.store[key]
	return obj, found
}

// List returns a snapshot of all objects currently in the cache.
func (c *ResourceCache) List() []runtime.Object {
	c.mu.RLock()
	defer c.mu.RUnlock()

	list := make([]runtime.Object, 0, len(c.store))
	for _, obj := range c.store {
		list = append(list, obj)
	}
	return list
}

// signalChange sends a non-blocking signal to changedCh.
func (c *ResourceCache) signalChange() {
	select {
	case c.changedCh <- struct{}{}:
	default:
	}
}

// AddEventHandler generates cache-updating event handlers.
func (c *ResourceCache) AddEventHandler(resourceType string) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			meta := k8s.GetObjectMeta(obj) // Use k8s.GetObjectMeta
			log.Debugf("ADD %s: %s/%s", resourceType, meta.Namespace, meta.Name)
			c.Upsert(obj.(runtime.Object))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			meta := k8s.GetObjectMeta(newObj) // Use k8s.GetObjectMeta
			log.Debugf("UPDATE %s: %s/%s", resourceType, meta.Namespace, meta.Name)
			c.Upsert(newObj.(runtime.Object))
		},
		DeleteFunc: func(obj interface{}) {
			meta := k8s.GetObjectMeta(obj) // Use k8s.GetObjectMeta
			log.Debugf("DELETE %s: %s/%s", resourceType, meta.Namespace, meta.Name)
			c.Delete(obj)
		},
	}
}

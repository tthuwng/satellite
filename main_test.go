package main

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	fake "k8s.io/client-go/kubernetes/fake"
	cachepkg "k8s.io/client-go/tools/cache"
)

func TestHandlerPutsObjectOnChannel(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	podInf := factory.Core().V1().Pods().Informer()

	cache := NewResourceCache()
	podInf.AddEventHandler(cache.AddEventHandler("Pod"))

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	cachepkg.WaitForCacheSync(stop, podInf.HasSynced)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unit-pod",
			Namespace: "default",
		},
	}
	_, _ = client.CoreV1().Pods("default").Create(
		context.TODO(), pod, metav1.CreateOptions{})

	time.Sleep(50 * time.Millisecond) // give informer time to process

	key := EntityKey{Kind: "Pod", Namespace: "default", Name: "unit-pod"}
	if _, found := cache.Get(key); !found {
		t.Fatalf("Pod %v not found in cache after Add event", key)
	}
}

func TestEventMatrix(t *testing.T) {
	cases := []struct {
		name string
		make runtime.Object
		edit func(runtime.Object)
	}{
		{
			name: "ConfigMap",
			make: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
				Data:       map[string]string{"k": "v"},
			},
			edit: func(o runtime.Object) {
				cm := o.(*corev1.ConfigMap)
				cm.Data["k"] = "v2"
			},
		},
	}

	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)

	cache := NewResourceCache()

	podInf := factory.Core().V1().Pods().Informer()
	podInf.AddEventHandler(cache.AddEventHandler("Pod"))
	rsInf := factory.Apps().V1().ReplicaSets().Informer()
	rsInf.AddEventHandler(cache.AddEventHandler("ReplicaSet"))
	deployInf := factory.Apps().V1().Deployments().Informer()
	deployInf.AddEventHandler(cache.AddEventHandler("Deployment"))
	nodeInf := factory.Core().V1().Nodes().Informer()
	nodeInf.AddEventHandler(cache.AddEventHandler("Node"))
	svcInf := factory.Core().V1().Services().Informer()
	svcInf.AddEventHandler(cache.AddEventHandler("Service"))
	cmInf := factory.Core().V1().ConfigMaps().Informer()
	cmInf.AddEventHandler(cache.AddEventHandler("ConfigMap"))

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)

	if !cachepkg.WaitForCacheSync(stop,
		podInf.HasSynced,
		rsInf.HasSynced,
		deployInf.HasSynced,
		nodeInf.HasSynced,
		svcInf.HasSynced,
		cmInf.HasSynced) {
		t.Fatal("Failed to sync caches")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.make.DeepCopyObject()
			meta := getObjectMeta(obj)
			key, _ := getKey(obj.(runtime.Object))

			switch o := obj.(type) {
			case *corev1.ConfigMap:
				_, _ = client.CoreV1().ConfigMaps(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
			}

			expectCacheState(t, cache, key, true)

			tc.edit(obj)
			meta = getObjectMeta(obj)

			switch o := obj.(type) {
			case *corev1.ConfigMap:
				_, _ = client.CoreV1().ConfigMaps(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
			}
			expectCacheState(t, cache, key, true)

			switch obj.(type) {
			case *corev1.ConfigMap:
				_ = client.CoreV1().ConfigMaps(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
			}
			expectCacheState(t, cache, key, false)
		})
	}
}

func expectCacheState(t *testing.T, cache *ResourceCache, key EntityKey, shouldExist bool) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)

	_, found := cache.Get(key)

	if found != shouldExist {
		if shouldExist {
			t.Errorf("Expected object with key %v to exist in cache, but it doesn't", key)
		} else {
			t.Errorf("Expected object with key %v NOT to exist in cache, but it does", key)
		}
	}
}

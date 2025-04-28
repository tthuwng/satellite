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
				ObjectMeta: metav1.ObjectMeta{Name: "cm-test", Namespace: "default"},
				Data:       map[string]string{"k": "v"},
			},
			edit: func(o runtime.Object) {
				cm := o.(*corev1.ConfigMap)
				cm.Data["k"] = "v2"
			},
		},
		{
			name: "Pod",
			make: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-test", Namespace: "default", Labels: map[string]string{"app": "test"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "nginx", Image: "nginx"}}},
			},
			edit: func(o runtime.Object) {
				pod := o.(*corev1.Pod)
				pod.Labels["updated"] = "true"
			},
		},
		{
			name: "ReplicaSet",
			make: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "rs-test", Namespace: "default"},
				Spec:       appsv1.ReplicaSetSpec{Replicas: int32Ptr(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "rs-test"}}},
			},
			edit: func(o runtime.Object) {
				rs := o.(*appsv1.ReplicaSet)
				*rs.Spec.Replicas = 2
			},
		},
		{
			name: "Deployment",
			make: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy-test", Namespace: "default"},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "deploy-test"}}},
			},
			edit: func(o runtime.Object) {
				deploy := o.(*appsv1.Deployment)
				*deploy.Spec.Replicas = 2
			},
		},
		{
			name: "Service",
			make: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc-test", Namespace: "default"},
				Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "test"}, Ports: []corev1.ServicePort{{Port: 80}}},
			},
			edit: func(o runtime.Object) {
				svc := o.(*corev1.Service)
				svc.Spec.Selector["updated"] = "true"
			},
		},
		{
			name: "Node", // Nodes are not namespaced
			make: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-test", Labels: map[string]string{"kubernetes.io/hostname": "node-test"}},
			},
			edit: func(o runtime.Object) {
				node := o.(*corev1.Node)
				node.Labels["updated"] = "true"
			},
		},
	}

	// Initialize fake client, factory, and cache
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	cache := NewResourceCache()

	// Add handlers
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

			// --- Create ---
			switch o := obj.(type) {
			case *corev1.ConfigMap:
				_, err := client.CoreV1().ConfigMaps(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Create failed: %v", err)
				}
			case *corev1.Pod:
				_, err := client.CoreV1().Pods(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Create failed: %v", err)
				}
			case *appsv1.ReplicaSet:
				_, err := client.AppsV1().ReplicaSets(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Create failed: %v", err)
				}
			case *appsv1.Deployment:
				_, err := client.AppsV1().Deployments(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Create failed: %v", err)
				}
			case *corev1.Service:
				_, err := client.CoreV1().Services(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Create failed: %v", err)
				}
			case *corev1.Node:
				_, err := client.CoreV1().Nodes().Create(context.TODO(), o, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Create failed: %v", err)
				}
			default:
				t.Fatalf("Create: Unhandled type %T", obj)
			}
			expectCacheState(t, cache, key, true) // Expect Added

			// --- Update ---
			tc.edit(obj)
			meta = getObjectMeta(obj) // Re-get meta after edit
			switch o := obj.(type) {
			case *corev1.ConfigMap:
				_, err := client.CoreV1().ConfigMaps(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Update failed: %v", err)
				}
			case *corev1.Pod:
				_, err := client.CoreV1().Pods(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Update failed: %v", err)
				}
			case *appsv1.ReplicaSet:
				_, err := client.AppsV1().ReplicaSets(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Update failed: %v", err)
				}
			case *appsv1.Deployment:
				_, err := client.AppsV1().Deployments(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Update failed: %v", err)
				}
			case *corev1.Service:
				_, err := client.CoreV1().Services(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Update failed: %v", err)
				}
			case *corev1.Node:
				_, err := client.CoreV1().Nodes().Update(context.TODO(), o, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Update failed: %v", err)
				}
			default:
				t.Fatalf("Update: Unhandled type %T", obj)
			}
			expectCacheState(t, cache, key, true) // Expect Updated (still present)

			// --- Delete ---
			switch obj.(type) {
			case *corev1.ConfigMap:
				err := client.CoreV1().ConfigMaps(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
			case *corev1.Pod:
				err := client.CoreV1().Pods(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
			case *appsv1.ReplicaSet:
				err := client.AppsV1().ReplicaSets(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
			case *appsv1.Deployment:
				err := client.AppsV1().Deployments(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
			case *corev1.Service:
				err := client.CoreV1().Services(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
			case *corev1.Node:
				err := client.CoreV1().Nodes().Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
			default:
				t.Fatalf("Delete: Unhandled type %T", obj)
			}
			expectCacheState(t, cache, key, false) // Expect Deleted
		})
	}
}

// Helper for ReplicaSet/Deployment spec
func int32Ptr(i int32) *int32 { return &i }

// expectCacheState checks if an object with the given key exists (or not) in the cache.
func expectCacheState(t *testing.T, cache *ResourceCache, key EntityKey, shouldExist bool) {
	t.Helper()
	// Increased delay slightly for fake client updates
	time.Sleep(100 * time.Millisecond)

	_, found := cache.Get(key)

	if found != shouldExist {
		if shouldExist {
			t.Errorf("Expected object with key %v to exist in cache, but it doesn't", key)
		} else {
			t.Errorf("Expected object with key %v NOT to exist in cache, but it does", key)
		}
	}
}

// Helper to get ObjectMeta (assuming it's moved to a shared location or duplicated here)
// func getObjectMeta(obj interface{}) metav1.ObjectMeta { ... }
// If getObjectMeta is not accessible, you might need to duplicate it or refactor it.

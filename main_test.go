package main

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	fake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func TestHandlerPutsObjectOnChannel(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	podInf := factory.Core().V1().Pods().Informer()

	updatesCh := make(chan runtime.Object, 1)

	podInf.AddEventHandler(newEventHandler("Pod", updatesCh))

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	cache.WaitForCacheSync(stop, podInf.HasSynced)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unit-pod",
			Namespace: "default",
		},
	}
	_, _ = client.CoreV1().Pods("default").Create(
		context.TODO(), pod, metav1.CreateOptions{})

	select {
	case obj := <-updatesCh:
		if meta := getObjectMeta(obj); meta.Name != "unit-pod" {
			t.Fatalf("expected unit-pod, got %s", meta.Name)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for pod event")
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
				cm := o.(*corev1.ConfigMap); cm.Data["k"] = "v2"
			},
		},
	}

	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	updates := make(chan runtime.Object, 10)

	podInf := factory.Core().V1().Pods().Informer()
	podInf.AddEventHandler(newEventHandler("Pod", updates))
	rsInf := factory.Apps().V1().ReplicaSets().Informer()
	rsInf.AddEventHandler(newEventHandler("ReplicaSet", updates))
	deployInf := factory.Apps().V1().Deployments().Informer()
	deployInf.AddEventHandler(newEventHandler("Deployment", updates))
	nodeInf := factory.Core().V1().Nodes().Informer()
	nodeInf.AddEventHandler(newEventHandler("Node", updates))
	svcInf := factory.Core().V1().Services().Informer()
	svcInf.AddEventHandler(newEventHandler("Service", updates))
	cmInf := factory.Core().V1().ConfigMaps().Informer()
	cmInf.AddEventHandler(newEventHandler("ConfigMap", updates))

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)

	if !cache.WaitForCacheSync(stop,
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
			switch o := obj.(type) {
			case *corev1.ConfigMap:
				_, _ = client.CoreV1().ConfigMaps(meta.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
			}

			expect("ADD", meta.Name, updates, t)

			tc.edit(obj)
			switch o := obj.(type) {
			case *corev1.ConfigMap:
				_, _ = client.CoreV1().ConfigMaps(meta.Namespace).Update(context.TODO(), o, metav1.UpdateOptions{})
			}
			expect("UPDATE", meta.Name, updates, t)

			switch obj.(type) {
			case *corev1.ConfigMap:
				_ = client.CoreV1().ConfigMaps(meta.Namespace).Delete(context.TODO(), meta.Name, metav1.DeleteOptions{})
			}
			expect("DELETE", meta.Name, updates, t)
		})
	}
}

func expect(event, name string, ch <-chan runtime.Object, t *testing.T) {
	select {
	case o := <-ch:
		if getObjectMeta(o).Name != name {
			t.Fatalf("%s: want %s, got %s", event, name, getObjectMeta(o).Name)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s: timed out", event)
	}
}

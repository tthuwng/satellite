package k8s

import (
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	cache "k8s.io/client-go/tools/cache"

	"satellite/internal/types"
)

// GetObjectMeta extracts ObjectMeta, handling tombstones.
// Keep the type switch for known types.
func GetObjectMeta(obj interface{}) metav1.ObjectMeta {
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
	case cache.DeletedFinalStateUnknown: // Handle Tombstone
		if o.Obj != nil {
			// Recursively call on the object within the tombstone
			return GetObjectMeta(o.Obj)
		} else {
			log.Warn("Tombstone object is nil")
			return metav1.ObjectMeta{}
		}
	default:
		log.Warnf("Unknown object type in GetObjectMeta: %T", obj)
		return metav1.ObjectMeta{}
	}
}

// GetKey extracts the EntityKey from a Kubernetes object.
func GetKey(obj runtime.Object) (types.EntityKey, bool) {
	meta := GetObjectMeta(obj)
	gvk := obj.GetObjectKind().GroupVersionKind()
	kind := gvk.Kind
	if kind == "" {
		kind = getKindFromType(obj)
		if kind == "" {
			log.Warnf("Could not determine Kind for object %s/%s", meta.Namespace, meta.Name)
			return types.EntityKey{}, false
		}
	}

	key := types.EntityKey{
		Kind:      kind,
		Namespace: meta.Namespace,
		Name:      meta.Name,
	}
	return key, true
}

// getKindFromType infers the Kind string from the object's Go type.
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

package graph

import (
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"satellite/internal/cache"
	"satellite/internal/k8s"
)

// Exported GraphEntityKey
type GraphEntityKey struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
}

// Exported GraphNode
type GraphNode struct {
	Key        GraphEntityKey    `json:"key"`
	Properties map[string]string `json:"properties"`
	Revision   uint64            `json:"revision"`
}

// Exported GraphRelationship
type GraphRelationship struct {
	Source           GraphEntityKey    `json:"source"`
	Target           GraphEntityKey    `json:"target"`
	RelationshipType string            `json:"relationshipType"`
	Properties       map[string]string `json:"properties,omitempty"`
	Revision         uint64            `json:"revision"`
}

// Exported Graph
type Graph struct {
	Nodes         []GraphNode         `json:"nodes"`
	Relationships []GraphRelationship `json:"relationships"`
	GraphRevision uint64              `json:"graphRevision"`
}

// Exported BuildGraph
func BuildGraph(resourceCache *cache.ResourceCache, currentGraphRevision uint64) Graph {
	graph := Graph{
		Nodes:         make([]GraphNode, 0),
		Relationships: make([]GraphRelationship, 0),
		GraphRevision: currentGraphRevision,
	}

	objects := resourceCache.List()

	// --- Node building ---
	for _, obj := range objects {
		key, ok := k8s.GetKey(obj)
		if !ok {
			log.Warnf("BuildGraph: Skipping object, could not get key for %T", obj)
			continue
		}

		graphKey := GraphEntityKey{
			Name:      key.Name,
			Namespace: key.Namespace,
			Kind:      key.Kind,
		}

		properties := extractProperties(obj)

		node := GraphNode{
			Key:        graphKey,
			Properties: properties,
			Revision:   currentGraphRevision,
		}
		graph.Nodes = append(graph.Nodes, node)
	}

	// --- Relationship building ---
	// lookups for efficient relationship finding
	podMap := make(map[GraphEntityKey]*corev1.Pod)
	for _, obj := range objects {
		if pod, ok := obj.(*corev1.Pod); ok {
			key, _ := k8s.GetKey(pod)
			graphKey := GraphEntityKey{Name: key.Name, Namespace: key.Namespace, Kind: key.Kind}
			podMap[graphKey] = pod
		}
	}

	for _, obj := range objects {
		sourceKey, ok := k8s.GetKey(obj)
		if !ok {
			continue
		}
		sourceGraphKey := GraphEntityKey{Name: sourceKey.Name, Namespace: sourceKey.Namespace, Kind: sourceKey.Kind}

		switch o := obj.(type) {
		case *corev1.Pod:
			// Pod -> ReplicaSet (OwnerReference)
			// Pod -> Deployment (OwnerReference - indirect via ReplicaSet)
			for _, ownerRef := range o.OwnerReferences {
				if ownerRef.Kind == "ReplicaSet" || ownerRef.Kind == "Deployment" {
					targetGraphKey := GraphEntityKey{
						Name:      ownerRef.Name,
						Namespace: o.Namespace,
						Kind:      ownerRef.Kind,
					}
					graph.Relationships = append(graph.Relationships, GraphRelationship{
						Source:           sourceGraphKey,
						Target:           targetGraphKey,
						RelationshipType: "OWNED_BY", // Pod is owned by RS/Deploy
						Revision:         currentGraphRevision,
					})
				}
			}

			// Pod -> Node (Scheduled On)
			if o.Spec.NodeName != "" {
				targetGraphKey := GraphEntityKey{
					Name: o.Spec.NodeName,
					Kind: "Node", // Nodes are not namespaced
				}
				graph.Relationships = append(graph.Relationships, GraphRelationship{
					Source:           sourceGraphKey,
					Target:           targetGraphKey,
					RelationshipType: "SCHEDULED_ON",
					Revision:         currentGraphRevision,
				})
			}

			// Pod -> ConfigMap (Mounts Volume)
			for _, vol := range o.Spec.Volumes {
				if vol.ConfigMap != nil {
					targetGraphKey := GraphEntityKey{
						Name:      vol.ConfigMap.Name,
						Namespace: o.Namespace,
						Kind:      "ConfigMap",
					}
					graph.Relationships = append(graph.Relationships, GraphRelationship{
						Source:           sourceGraphKey,
						Target:           targetGraphKey,
						RelationshipType: "MOUNTS",
						Revision:         currentGraphRevision,
					})
				}
			}

		case *appsv1.ReplicaSet:
			// ReplicaSet -> Deployment (OwnerReference)
			for _, ownerRef := range o.OwnerReferences {
				if ownerRef.Kind == "Deployment" {
					targetGraphKey := GraphEntityKey{
						Name:      ownerRef.Name,
						Namespace: o.Namespace,
						Kind:      ownerRef.Kind,
					}
					graph.Relationships = append(graph.Relationships, GraphRelationship{
						Source:           sourceGraphKey,
						Target:           targetGraphKey,
						RelationshipType: "OWNED_BY", // RS is owned by Deploy
						Revision:         currentGraphRevision,
					})
				}
			}
			// ReplicaSet -> Pod (Owns) - Implicitly handled by Pod -> ReplicaSet

		case *appsv1.Deployment:
			// Deployment -> ReplicaSet (Owns) - Implicitly handled by ReplicaSet -> Deployment

		case *corev1.Service:
			// Service -> Pod (Selector)
			if o.Spec.Selector != nil && len(o.Spec.Selector) > 0 {
				sel := labels.SelectorFromSet(o.Spec.Selector)
				for podKey, pod := range podMap {
					// Check namespace match before label match
					if pod.Namespace == o.Namespace && sel.Matches(labels.Set(pod.Labels)) {
						graph.Relationships = append(graph.Relationships, GraphRelationship{
							Source:           sourceGraphKey,
							Target:           podKey,
							RelationshipType: "SELECTS",
							Revision:         currentGraphRevision,
						})
					}
				}
			}

			// Node and ConfigMap do not originate relationships in this model
		}
	}

	log.Infof("Built graph revision %d with %d nodes and %d relationships",
		currentGraphRevision, len(graph.Nodes), len(graph.Relationships))

	return graph
}

func int32PtrToString(ptr *int32) string {
	if ptr == nil {
		return ""
	}
	return fmt.Sprintf("%d", *ptr)
}

func timePtrToString(ptr *metav1.Time) string {
	if ptr == nil {
		return ""
	}
	return ptr.Format(time.RFC3339)
}

// converts relevant fields from a runtime.Object into a flat map.
func extractProperties(obj runtime.Object) map[string]string {
	props := make(map[string]string)
	meta := k8s.GetObjectMeta(obj)

	// common properties
	props["uid"] = string(meta.UID)
	props["resourceVersion"] = meta.ResourceVersion
	props["creationTimestamp"] = meta.CreationTimestamp.String()
	if len(meta.Labels) > 0 {
		props["labels"] = labels.Set(meta.Labels).String()
	}
	if len(meta.Annotations) > 0 {
		annoStrings := []string{}
		for k, v := range meta.Annotations {
			annoStrings = append(annoStrings, fmt.Sprintf("%s=%s", k, v))
		}
		props["annotations"] = strings.Join(annoStrings, ",")
	}

	// type-specific properties
	switch o := obj.(type) {
	case *corev1.Pod:
		props["status.phase"] = string(o.Status.Phase)
		props["spec.nodeName"] = o.Spec.NodeName
		props["status.podIP"] = o.Status.PodIP
		props["status.hostIP"] = o.Status.HostIP
		props["status.startTime"] = timePtrToString(o.Status.StartTime)

	case *appsv1.ReplicaSet:
		props["spec.replicas"] = int32PtrToString(o.Spec.Replicas)
		props["status.replicas"] = fmt.Sprintf("%d", o.Status.Replicas)
		props["status.readyReplicas"] = fmt.Sprintf("%d", o.Status.ReadyReplicas)
		props["status.availableReplicas"] = fmt.Sprintf("%d", o.Status.AvailableReplicas)
		if o.Spec.Selector != nil {
			props["spec.selector"] = labels.SelectorFromSet(o.Spec.Selector.MatchLabels).String()
		} else {
			props["spec.selector"] = ""
		}

	case *appsv1.Deployment:
		props["spec.replicas"] = int32PtrToString(o.Spec.Replicas)
		props["status.replicas"] = fmt.Sprintf("%d", o.Status.Replicas)
		props["status.updatedReplicas"] = fmt.Sprintf("%d", o.Status.UpdatedReplicas)
		props["status.readyReplicas"] = fmt.Sprintf("%d", o.Status.ReadyReplicas)
		props["status.availableReplicas"] = fmt.Sprintf("%d", o.Status.AvailableReplicas)
		if o.Spec.Selector != nil {
			props["spec.selector"] = labels.SelectorFromSet(o.Spec.Selector.MatchLabels).String()
		} else {
			props["spec.selector"] = ""
		}

	case *corev1.Node:
		props["spec.podCIDR"] = o.Spec.PodCIDR
		props["status.capacity.cpu"] = o.Status.Capacity.Cpu().String()
		props["status.capacity.memory"] = o.Status.Capacity.Memory().String()
		props["status.allocatable.cpu"] = o.Status.Allocatable.Cpu().String()
		props["status.allocatable.memory"] = o.Status.Allocatable.Memory().String()
		props["status.nodeInfo.kubeletVersion"] = o.Status.NodeInfo.KubeletVersion
		props["status.nodeInfo.osImage"] = o.Status.NodeInfo.OSImage
		props["status.nodeInfo.containerRuntimeVersion"] = o.Status.NodeInfo.ContainerRuntimeVersion

	case *corev1.Service:
		props["spec.type"] = string(o.Spec.Type)
		props["spec.clusterIP"] = o.Spec.ClusterIP
		if len(o.Spec.ClusterIPs) > 0 {
			props["spec.clusterIPs"] = strings.Join(o.Spec.ClusterIPs, ",")
		}
		if o.Spec.Selector != nil {
			props["spec.selector"] = labels.Set(o.Spec.Selector).String()
		}

	case *corev1.ConfigMap:
		if len(o.Data) > 0 {
			keys := make([]string, 0, len(o.Data))
			for k := range o.Data {
				keys = append(keys, k)
			}
			props["data.keys"] = strings.Join(keys, ",")
		}

	default:
		log.Debugf("extractProperties: Unhandled type %T", obj)
	}

	return props
}

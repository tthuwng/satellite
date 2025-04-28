package main

import (
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

type GraphEntityKey struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
}

// represents a resource in the graph.
type GraphNode struct {
	Key        GraphEntityKey    `json:"key"`
	Properties map[string]string `json:"properties"`
	Revision   uint64            `json:"revision"`
}

// represents a connection between two resources.
type GraphRelationship struct {
	Source           GraphEntityKey    `json:"source"`
	Target           GraphEntityKey    `json:"target"`
	RelationshipType string            `json:"relationshipType"`
	Properties       map[string]string `json:"properties,omitempty"`
	Revision         uint64            `json:"revision"`
}

// represents the overall structure of nodes and relationships.
type Graph struct {
	Nodes         []GraphNode         `json:"nodes"`
	Relationships []GraphRelationship `json:"relationships"`
	GraphRevision uint64              `json:"graphRevision"`
}

// constructs the graph representation from the current cache state.
func BuildGraph(cache *ResourceCache, currentGraphRevision uint64) Graph {
	graph := Graph{
		Nodes:         make([]GraphNode, 0),
		Relationships: make([]GraphRelationship, 0),
		GraphRevision: currentGraphRevision,
	}

	objects := cache.List()

	for _, obj := range objects {
		// runtime.Object -> GraphNode
		key, ok := getKey(obj)
		if !ok {
			log.Printf("BuildGraph: Skipping object, could not get key for %T\n", obj)
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
			key, _ := getKey(pod)
			graphKey := GraphEntityKey{Name: key.Name, Namespace: key.Namespace, Kind: key.Kind}
			podMap[graphKey] = pod
		}
	}

	for _, obj := range objects {
		sourceKey, ok := getKey(obj)
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

	log.Printf("Built graph revision %d with %d nodes and %d relationships\n",
		currentGraphRevision, len(graph.Nodes), len(graph.Relationships))

	return graph
}

// converts relevant fields from a runtime.Object into a flat map.
// TODO: Implement actual property extraction for each resource type.
func extractProperties(obj runtime.Object) map[string]string {
	props := make(map[string]string)
	meta := getObjectMeta(obj)

	props["uid"] = string(meta.UID)
	props["resourceVersion"] = meta.ResourceVersion
	props["creationTimestamp"] = meta.CreationTimestamp.String()

	// TODO: Add type-specific properties
	// switch o := obj.(type) {
	// case *corev1.Pod:
	//  props["status.phase"] = string(o.Status.Phase)
	//  props["spec.nodeName"] = o.Spec.NodeName
	// ... etc
	// }

	return props
}

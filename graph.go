package main

import (
	"log"

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

	// TODO: build relationships

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

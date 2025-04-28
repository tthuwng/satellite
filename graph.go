package main

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

package main

import (
	"fmt"
	"log"
	"strings"

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
func extractProperties(obj runtime.Object) map[string]string {
	props := make(map[string]string)
	meta := getObjectMeta(obj) // Assumes getObjectMeta handles tombstones

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
		props["spec.nodeName"] = o.Spec.NodeName
		props["status.phase"] = string(o.Status.Phase)
		props["status.hostIP"] = o.Status.HostIP
		props["status.podIP"] = o.Status.PodIP
		if o.Status.StartTime != nil {
			props["status.startTime"] = o.Status.StartTime.String()
		}

	case *appsv1.ReplicaSet:
		props["spec.replicas"] = fmt.Sprintf("%d", *o.Spec.Replicas)
		props["status.replicas"] = fmt.Sprintf("%d", o.Status.Replicas)
		props["status.readyReplicas"] = fmt.Sprintf("%d", o.Status.ReadyReplicas)
		props["status.availableReplicas"] = fmt.Sprintf("%d", o.Status.AvailableReplicas)
		if o.Spec.Selector != nil {
			props["spec.selector"] = labels.SelectorFromSet(o.Spec.Selector.MatchLabels).String()
		}

	case *appsv1.Deployment:
		props["spec.replicas"] = fmt.Sprintf("%d", *o.Spec.Replicas)
		props["status.replicas"] = fmt.Sprintf("%d", o.Status.Replicas)
		props["status.updatedReplicas"] = fmt.Sprintf("%d", o.Status.UpdatedReplicas)
		props["status.readyReplicas"] = fmt.Sprintf("%d", o.Status.ReadyReplicas)
		props["status.availableReplicas"] = fmt.Sprintf("%d", o.Status.AvailableReplicas)
		if o.Spec.Selector != nil {
			props["spec.selector"] = labels.SelectorFromSet(o.Spec.Selector.MatchLabels).String()
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
		log.Printf("extractProperties: Unhandled type %T", obj)
	}

	return props
}

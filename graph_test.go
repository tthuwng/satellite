package main

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildGraph_Relationships(t *testing.T) {
	// --- Test Data Setup ---
	ns := "graph-test"
	nodeName := "test-node"

	// Deployment
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: ns, UID: types.UID("deploy-uid")},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}}},
	}
	deployKey := EntityKey{Kind: "Deployment", Namespace: ns, Name: "test-deploy"}
	deployGraphKey := GraphEntityKey{Kind: "Deployment", Namespace: ns, Name: "test-deploy"}

	// ReplicaSet owned by Deployment
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-rs",
			Namespace:       ns,
			UID:             types.UID("rs-uid"),
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: deploy.Name, UID: deploy.UID}},
		},
		Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}}},
	}
	rsKey := EntityKey{Kind: "ReplicaSet", Namespace: ns, Name: "test-rs"}
	rsGraphKey := GraphEntityKey{Kind: "ReplicaSet", Namespace: ns, Name: "test-rs"}

	// Pod owned by ReplicaSet, scheduled on Node, mounting ConfigMap
	cmName := "test-cm"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod",
			Namespace:       ns,
			UID:             types.UID("pod-uid"),
			Labels:          map[string]string{"app": "test"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID}},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Volumes:  []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cmName}}}}},
		},
	}
	podKey := EntityKey{Kind: "Pod", Namespace: ns, Name: "test-pod"}
	podGraphKey := GraphEntityKey{Kind: "Pod", Namespace: ns, Name: "test-pod"}

	// Node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, UID: types.UID("node-uid")},
	}
	nodeKey := EntityKey{Kind: "Node", Name: nodeName}
	nodeGraphKey := GraphEntityKey{Kind: "Node", Name: nodeName}

	// Service selecting Pod
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: ns, UID: types.UID("svc-uid")},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "test"}},
	}
	svcKey := EntityKey{Kind: "Service", Namespace: ns, Name: "test-svc"}
	svcGraphKey := GraphEntityKey{Kind: "Service", Namespace: ns, Name: "test-svc"}

	// ConfigMap mounted by Pod
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns, UID: types.UID("cm-uid")},
	}
	cmKey := EntityKey{Kind: "ConfigMap", Namespace: ns, Name: cmName}
	cmGraphKey := GraphEntityKey{Kind: "ConfigMap", Namespace: ns, Name: cmName}

	// --- Cache Setup ---
	cache := NewResourceCache()
	cache.store[deployKey] = deploy
	cache.store[rsKey] = rs
	cache.store[podKey] = pod
	cache.store[nodeKey] = node
	cache.store[svcKey] = svc
	cache.store[cmKey] = cm

	// --- Build Graph ---
	graphRevision := uint64(1)
	graph := BuildGraph(cache, graphRevision)

	// --- Assertions ---
	if len(graph.Nodes) != 6 {
		t.Fatalf("Expected 6 nodes, got %d", len(graph.Nodes))
	}
	// Check relationship count before detailed check
	if len(graph.Relationships) != 5 { // Pod->RS, RS->Deploy, Pod->Node, Pod->CM, Svc->Pod
		t.Fatalf("Expected 5 relationships, got %d. Relationships: %+v", len(graph.Relationships), graph.Relationships)
	}

	expectedRelationships := map[string]GraphRelationship{
		"pod-owned-by-rs":       {Source: podGraphKey, Target: rsGraphKey, RelationshipType: "OWNED_BY"},
		"rs-owned-by-deploy":    {Source: rsGraphKey, Target: deployGraphKey, RelationshipType: "OWNED_BY"},
		"pod-scheduled-on-node": {Source: podGraphKey, Target: nodeGraphKey, RelationshipType: "SCHEDULED_ON"},
		"pod-mounts-cm":         {Source: podGraphKey, Target: cmGraphKey, RelationshipType: "MOUNTS"},
		"svc-selects-pod":       {Source: svcGraphKey, Target: podGraphKey, RelationshipType: "SELECTS"},
	}

	foundRelationships := make(map[string]bool)
	for _, rel := range graph.Relationships {
		found := false
		for name, expected := range expectedRelationships {
			if rel.Source == expected.Source && rel.Target == expected.Target && rel.RelationshipType == expected.RelationshipType {
				if foundRelationships[name] {
					t.Errorf("Duplicate relationship found for %s", name)
				}
				foundRelationships[name] = true
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected relationship found: %+v", rel)
		}
	}

	if len(foundRelationships) != len(expectedRelationships) {
		t.Errorf("Expected %d relationships, but found %d unique matching expected relationships", len(expectedRelationships), len(foundRelationships))
		for name := range expectedRelationships {
			if !foundRelationships[name] {
				t.Errorf("Missing expected relationship: %s (%+v -> %+v)", name, expectedRelationships[name].Source, expectedRelationships[name].Target)
			}
		}
	}

	// Check revisions
	for _, node := range graph.Nodes {
		if node.Revision != graphRevision {
			t.Errorf("Node %+v has incorrect revision %d, expected %d", node.Key, node.Revision, graphRevision)
		}
	}
	for _, rel := range graph.Relationships {
		if rel.Revision != graphRevision {
			t.Errorf("Relationship %+v -> %+v has incorrect revision %d, expected %d", rel.Source, rel.Target, rel.Revision, graphRevision)
		}
	}
}

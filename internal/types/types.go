package types

// EntityKey uniquely identifies a Kubernetes resource.
type EntityKey struct {
	Kind      string
	Namespace string // Empty for non-namespaced resources like Node
	Name      string
}

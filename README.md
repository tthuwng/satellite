# Satellite - High Performance Kubernetes Scraper Summary

## 1. Project Goal

Develop a lightweight, high-performance service ("Satellite") that:
*   Connects to a single Kubernetes cluster.
*   Extracts metadata for key resource types (Pods, ReplicaSets, Deployments, Nodes, Services, ConfigMaps).
*   Transforms this data into a relationship graph.
*   Efficiently emits versioned snapshots of this graph as JSON files.
*   Minimizes load on the Kubernetes API server.

## 2. Approach & Design Choices

The implementation leverages Kubernetes' native event-driven mechanisms for efficiency:

*   **Core Mechanism:** Utilizes `client-go`'s `SharedInformerFactory` to establish watches on the target resource types. This avoids polling (`List()` in a loop) and provides near real-time updates with minimal API server load.
*   **Caching:** An in-memory `ResourceCache` (using `map[types.EntityKey]runtime.Object` protected by `sync.RWMutex`) stores the latest known state of each observed object. Event handlers associated with the informers perform atomic upserts/deletes on this cache.
*   **Graph Building:** Triggered reactively whenever the `ResourceCache` signals a change. The `BuildGraph` function:
    *   Iterates through the current cache snapshot to create `GraphNode` representations.
    *   Derives `GraphRelationship` entries based on standard Kubernetes fields:
        *   `OwnerReferences` (for `OWNED_BY` relationships: Pod -> RS, RS -> Deploy).
        *   `Pod.spec.nodeName` (for `SCHEDULED_ON`).
        *   `Pod.spec.volumes` (for `MOUNTS` ConfigMap).
        *   `Service.spec.selector` (for `SELECTS` Pods).
*   **Emission:**
    *   Generates a `Graph` struct containing nodes, relationships, and a monotonically increasing `GraphRevision`.
    *   Marshals the `Graph` to indented JSON.
    *   Writes atomically to a timestamped file (`graph-YYYYMMDD-HHMMSS.json`) in a configurable output directory using a temporary file + `os.Rename`.
*   **Language & Libraries:** Implemented in Go, using `k8s.io/client-go`, `logrus` for leveled logging, and standard library features (`flag`, `sync`, `os/signal`).
*   **Structure:** Standard Go project layout (`cmd/satellite`, `internal/...`).

## 3. Key Assumptions & Simplifications

*   **Resource Scope:** Limited to the 6 specified resource types.
*   **Relationship Scope:** Focused on common, direct relationships.
*   **Persistence:** Simple local file output assumes downstream processing handles consumption/indexing.
*   **Cache Content:** Stores the full `runtime.Object` for simplicity, rather than a minimized version.
*   **Graph Properties:** Flattened resource fields into `map[string]string` for properties.
*   **Error Handling:** Relies on `logrus` for logging errors; critical startup errors cause termination (`Fatalf`). Assumes informers handle transient watch errors internally.

## 4. Testing Strategy

*   **Unit Tests:**
    *   `tests/main_test.go`: Verifies informer event handlers correctly update the cache using `fake.Clientset`.
    *   `tests/graph_test.go`: Verifies `BuildGraph` correctly generates nodes and relationships for a known set of objects.
*   **Smoke Tests (Manual):** Executed against a local Minikube cluster:
    1.  Start Satellite, verify initial graph emission.
    2.  Create sample workloads (`Deployment`, `Service`, `ConfigMap`).
    3.  Perform mutations (`scale`, `edit` deploy to mount volume, `delete` service, `delete` deploy).
    4.  Verify graph files are emitted corresponding to changes, inspect contents for correctness (nodes, relationships, revisions).
    5.  Verify graceful shutdown (`Ctrl+C`) emits a final graph.

## 5. Future Improvements & Considerations

*   **Scalability:** For very large clusters, explore sharding (per-namespace Satellites) and central aggregation (e.g., Kafka).
*   **Efficiency:** 
    *   Emit binary diffs instead of full snapshots.
    *   Implement `SimplifiedObject` cache storage to reduce memory.
    *   Optimize cache change detection (signal only on actual content change).
*   **Extensibility:** Add support for Custom Resource Definitions (CRDs) via generic informers.
*   **Security:** Implement fine-grained RBAC using a dedicated ServiceAccount.
*   **Observability:** Add Prometheus metrics (cache size, object counts, build/emit latency, informer health).
*   **Robustness:** More sophisticated error handling (retries, specific API error handling).
*   **Data Model:** Option for richer, structured properties (`map[string]interface{}`), potentially more relationship types.
*   **Deployment:** Provide Dockerfile and sample Kubernetes deployment manifests.
*   **Performance Testing:** Benchmark resource usage and API load against specific targets. 
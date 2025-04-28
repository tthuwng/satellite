# Satellite TODO List

## Phase 1: Core Implementation (Steps 3.1 - 3.7)

-   [x] **3.1 Bootstrap:** Implement logic to connect to Kubernetes API (in-cluster or local kubeconfig).
-   [x] **3.2 Set up informers:**
    -   [x] Initialize `SharedInformerFactory`.
    -   [x] Get informers for Pods, ReplicaSets, Deployments, Nodes, Services, ConfigMaps.
-   [x] **3.3 Attach handlers:**
    -   [x] Create an `updatesCh` channel.
    -   [x] Implement `OnAdd`, `OnUpdate`, `OnDelete` handlers for each informer to push objects to `updatesCh`.
-   [x] **3.4 Cache layer:**
    -   [x] Define `EntityKey` struct (`{type, namespace, name}`).
    -   [x] Implement a cache map (`map[EntityKey]SimplifiedObject`). // Storing runtime.Object for now
    -   [x] Define `SimplifiedObject` struct containing only necessary metadata (e.g., `ObjectMeta`, relevant spec/status fields). // Deferred this, storing full object
    -   [x] Implement logic to process `updatesCh` and update the cache. // Handlers update cache directly
-   [x] **3.5 Graph builder:**
    -   [x] Define `Graph`, `GraphNode`, `GraphRelationship` structs matching the spec.
    -   [x] Implement function `BuildGraph(cache map[EntityKey]SimplifiedObject) Graph`. // Skeleton + basic node creation
    -   [x] Iterate cache to create `[]GraphNode`. // Basic node creation done
    -   [x] Implement relationship derivation logic:
        -   [x] Pod -> ReplicaSet (`ownerReferences`).
        -   [x] ReplicaSet -> Deployment (`ownerReferences`).
        -   [x] Pod -> Node (`spec.nodeName`).
        -   [x] Service -> Pod (label selectors).
        -   [x] Pod -> ConfigMap (`volumes`).
-   [x] **3.6 Revision logic:**
    -   [x] Add a global `revision` counter (`uint64`).
    -   [x] Increment `revision` on every cache change.
    -   [x] Stamp `revision` on nodes/relationships affected by the change (or simply on the whole graph for simplicity initially). // Stamping whole graph
-   [x] **3.7 Emit:**
    -   [x] Implement JSON marshaling (consider `jsoniter`). // using standard json for now
    -   [x] Implement file writing logic:
        -   [x] Generate versioned filename (`graph-YYYYMMDD-HHMMSS.jsonl`). // Using .json
        -   [x] Write to a temporary file.
        -   [x] Use `os.Rename` for atomic write.
    -   [x] Trigger emit periodically or based on cache changes. // triggered on cache change

## Phase 2: CLI & Operability (Steps 3.8 - 3.9)

-   [x] **3.8 CLI flags:**
    -   [x] Add flags for `--output-dir`.
    -   [ ] Add flag for `--emit-frequency`. // Skipped for now, using event-driven model
    -   [x] Add flag for `--log-level`. // Filtering implemented with logrus
-   [x] **3.9 Graceful shutdown:**
    -   [x] Implement signal handling (SIGINT, SIGTERM).
    -   [x] Ensure informers are stopped.
    -   [x] Ensure the last graph state is flushed before exiting.

## Phase 3: Testing (Step 4)

-   [x] **4.1 Set up Minikube:** Have a local cluster running.
-   [x] **4.2 Run Satellite:** Test basic execution and file output.
-   [ ] **4.3 Mutation smoke-tests:**
    -   [ ] Test scaling a Deployment.
    -   [ ] Test deleting a Service.
    -   [ ] Test creating/mounting a ConfigMap.
    -   [ ] Verify graph updates and revision increments for each mutation.
-   [x] **4.4 Unit tests:**
    -   [ ] Set up fake client (`k8s.io/client-go/kubernetes/fake`).
    -   [x] Write unit tests for graph builder logic (especially relationship rules).
    -   [ ] Write table-driven tests for relationship derivations.

## Phase 4: Documentation & Refinement (Step 6 from Plan)

-   [ ] Write 1-page summary covering future work, testing strategy, etc.
-   [ ] Review resource usage (memory/CPU).
-   [ ] Add basic Prometheus metrics (optional, from future work).
-   [ ] Refine RBAC permissions (optional, from future work).

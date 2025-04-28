# Satellite - Kubernetes Resource Graph Extractor

`satellite` watches a Kubernetes cluster for changes to common resource types (Pods, Deployments, Services, etc.) and periodically emits a JSON file representing the current state and relationships between these resources.

It's designed to be lightweight and place minimal load on the Kubernetes API server by leveraging the watch mechanism.

## Features

*   Watches Pods, ReplicaSets, Deployments, Nodes, Services, ConfigMaps.
*   Builds a graph representing relationships (`OWNED_BY`, `SCHEDULED_ON`, `MOUNTS`, `SELECTS`).
*   Emits graph state as timestamped JSON files (e.g., `graph-YYYYMMDD-HHMMSS.json`).
*   **Event-driven & Optimized:** Graph generation is triggered by actual cache changes (based on `ResourceVersion`), minimizing unnecessary work.
*   Atomic file writes using temporary files.
*   Configurable output directory (`--output-dir`).
*   Configurable log level (`--log-level` using `logrus`: debug, info, warn, error, fatal, panic).
*   Graceful shutdown (emits final graph state).

## Architecture Overview

Follows standard Go project structure:

*   **`cmd/satellite`**: Main application entry point, handles flags, signal handling, and orchestrates components.
*   **`internal/cache`**: Implements the thread-safe, in-memory cache (`ResourceCache`) storing `runtime.Object` instances, keyed by `types.EntityKey`. Signals changes based on `ResourceVersion`.
*   **`internal/graph`**: Defines graph data structures (`Graph`, `GraphNode`, etc.) and the `BuildGraph` logic for converting cached objects into graph nodes and deriving relationships.
*   **`internal/emitter`**: Handles atomic writing of the marshalled graph JSON to timestamped files.
*   **`internal/k8s`**: Utility functions for interacting with Kubernetes objects (e.g., `GetObjectMeta`, `GetKey`).
*   **`internal/types`**: Defines shared core types like `EntityKey`.
*   **`tests`**: Contains external test packages (`*_test.go` files).

## Getting Started

### Prerequisites

*   Go (version specified in `go.mod`)
*   Access to a Kubernetes cluster (e.g., Minikube, Kind, Docker Desktop)
*   `kubectl` configured to access the cluster.
*   `make` (optional, for convenience)

### Build

```bash
make build
# Or:
# go build -o satellite ./cmd/satellite
```

### Run

```bash
# Run with defaults (output to ./data, log level info)
./satellite

# Specify output directory and debug logging
./satellite --output-dir /tmp/satellite-output --log-level debug

# Use make (passes arguments via ARGS)
# make run ARGS="--output-dir /tmp/satellite-output --log-level debug"
```

Satellite connects using default kubeconfig or in-cluster config. It logs status and emits graph files to the output directory on detected changes.

Press `Ctrl+C` for graceful shutdown.

## Testing

*   **Unit Tests:** Run logic tests for cache, graph building, etc.
    ```bash
    make test
    # Or:
    # go test ./...
    ```
*   **Smoke Test Script (`smoke_test.sh`):** An automated end-to-end test using Minikube.
    *   Ensures Minikube is running.
    *   Applies a simple nginx workload.
    *   Runs `satellite` in the background.
    *   Waits, verifies graph output, and performs basic checks.
    *   Cleans up.
    *   Run with: `sh smoke_test.sh` (can set `PROFILE` env var for different Minikube profile).

## Visualization (Optional)

```bash
# Build the binary
make build

# Run the satellite binary
./satellite --output-dir ./satellite-data

# Copy the graph file to the current directory
cp ./satellite-data/graph-*.json ./data

# Visualize the graph
make view
```

A simple web-based visualizer (`viz.html`) is included:

1.  Ensure `jq` is installed.
2.  Run `sh viz.sh` after `satellite` has produced at least one graph file. This creates `graph.json` using the *latest* graph file found in `./data` (or the directory specified by the first argument to `viz.sh`).
3.  Serve the directory locally (e.g., `python3 -m http.server 8080`).
4.  Open `http://localhost:8080/viz.html` in your browser.

*(Note: The `viz.sh` script makes assumptions about linking nodes based on keys; adjustments might be needed for complex scenarios.)*

## Future Considerations

*   Add Prometheus metrics.
*   Support for Custom Resources (CRDs).
*   Fine-grained RBAC.
*   Emit binary diffs for efficiency.
*   Containerization (Dockerfile) and Helm chart.
*   More sophisticated cache change detection (deep equality check as fallback?). 
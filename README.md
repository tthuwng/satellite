# Satellite - Kubernetes Resource Graph Extractor


`satellite` watches a Kubernetes cluster for changes to common resource types (Pods, Deployments, Services, etc.) and periodically emits a JSON file representing the current state and relationships between these resources.

It's designed to be lightweight and place minimal load on the Kubernetes API server by leveraging the watch mechanism.

## Features

*   Watches Pods, ReplicaSets, Deployments, Nodes, Services, ConfigMaps.
*   Builds a graph representing relationships (`OWNED_BY`, `SCHEDULED_ON`, `MOUNTS`, `SELECTS`).
*   Emits graph state as timestamped JSON files (e.g., `graph-YYYYMMDD-HHMMSS.json`).
*   Event-driven graph generation (only builds/emits when changes occur).
*   Atomic file writes using temporary files.
*   Configurable output directory and log level.
*   Graceful shutdown (emits final graph state).

## Architecture Overview

1.  **Informers:** Uses `client-go` `SharedInformerFactory` to watch resource changes.
2.  **Cache:** An internal, thread-safe cache (`internal/cache`) stores the latest state of known objects.
3.  **Graph Builder:** (`internal/graph`) Rebuilds the node/relationship graph when the cache signals a change.
4.  **Emitter:** (`internal/emitter`) Marshals the graph to JSON and writes it atomically to disk.
5.  **Main:** (`cmd/satellite`) Ties everything together, handles CLI flags and signals.

## Getting Started

### Prerequisites

*   Go 1.21+
*   Access to a Kubernetes cluster (e.g., Minikube, Kind, Docker Desktop)
*   `kubectl` configured to access the cluster.
*   `make` (optional, for convenience)

### Build

```bash
make build
# Or:
# go build -o satellite ./cmd/satellite/main.go
```

### Run

```bash
# Run with defaults (output to ./data, log level info)
./satellite

# Specify output directory and log level
./satellite --output-dir /tmp/satellite-output --log-level debug

# Use make (passes arguments via ARGS)
# make run ARGS="--output-dir /tmp/satellite-output --log-level debug"
```

Satellite will connect to the cluster specified in your default kubeconfig context (or use in-cluster config if run inside a pod). It will log cache sync status and then start emitting graph files to the specified output directory whenever cluster changes are detected.

Press `Ctrl+C` to shut down gracefully (it will attempt to emit one final graph).

## Testing

*   **Unit Tests:** Run the core logic tests.
    ```bash
    make test
    # Or:
    # go test ./...
    ```
*   **Smoke Test Script (`smoke_test.sh`):** An end-to-end test using Minikube.
    *   Ensures Minikube (profile `minikube` by default) is running.
    *   Kills any previous satellite processes.
    *   Applies a simple nginx Deployment and Service.
    *   Runs `satellite` in the background, logging to INFO and outputting to a temporary directory.
    *   Waits for the first graph file to appear and then observes for a set duration (`WAIT_SECS`).
    *   Sends SIGINT for graceful shutdown and waits for the process.
    *   Checks the latest graph file for a minimum number of nodes and relationships.
    *   Cleans up the nginx workload and temporary directory.
    *   Run with: `sh smoke_test.sh`
## Visualization (Optional)

A simple web-based visualizer is included:

1.  Ensure `jq` is installed.
2.  Run `sh viz.sh` after `satellite` has produced at least one `graph-*.json` file in the output directory (default `./data`). This creates `graph.json`.
3.  Serve the directory locally (e.g., `python3 -m http.server 8080`).
4.  Open `http://localhost:8080/vis.html` in your browser.

*(Note: The visualizer might require adjustments based on the exact structure of `graph.json`)*

## Future Considerations

*   Add Prometheus metrics.
*   Support for Custom Resources (CRDs).
*   Fine-grained RBAC for security.
*   Optimize cache change detection.
*   Emit binary diffs for efficiency.
*   Containerization (Dockerfile) and Helm chart. 
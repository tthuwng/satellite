#!/usr/bin/env bash
# ---------------------------------------------------------------
# Convert the newest graph-*.json → graph.json for vis-network
# ---------------------------------------------------------------
set -euo pipefail

GRAPH_DIR=${1:-data}                         # where Satellite writes files
GRAPH_FILE=$(ls -1t "${GRAPH_DIR}"/graph-*.json | head -1)

[ -z "${GRAPH_FILE:-}" ] && { echo "no graph files found"; exit 1; }

echo "📄  Using ${GRAPH_FILE}"

jq '
  def node_id(k):
    "\(k.kind)/\((k.namespace // "_"))/\(k.name)";

  {
    nodes: (.nodes
      | map({
          id:    (node_id(.key)),
          label: (.key.kind + "\n" + .key.name)
        })
    ),

    edges: (.relationships
      | map({
          from: (node_id(.source)),
          to:   (node_id(.target)),
          label: .relationshipType
        })
    )
  }
' "${GRAPH_FILE}" > graph.json

echo "✅  graph.json written – open vis.html 🚀"

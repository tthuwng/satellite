#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# smoke_test.sh – idempotent end-to-end sanity check for Satellite + Minikube
# ---------------------------------------------------------------------------
set -euo pipefail

##############################################################################
# Tunables (override with env vars)
##############################################################################
PROFILE=${PROFILE:-minikube}      # `PROFILE=myprofile ./smoke_test.sh`
WAIT_SECS=${WAIT_SECS:-30}        # seconds Satellite should observe the cluster
MIN_NODES=${MIN_NODES:-10}        # floor for node count in the graph
MIN_RELS=${MIN_RELS:-5}           # floor for relationship count

##############################################################################
# 0. Bootstrap / ensure cluster
##############################################################################
export MINIKUBE_IN_STYLE=false    # quieter logs, no ANSI art

echo "😄  minikube $(minikube version | head -1)"
minikube status -p "$PROFILE" >/dev/null 2>&1 || \
  minikube start -p "$PROFILE" --driver=docker

kubectl config use-context "$PROFILE" >/dev/null

##############################################################################
# 1. Deploy a tiny workload (safe to re-apply)
##############################################################################
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata: { name: nginx }
spec:
  replicas: 1
  selector: { matchLabels: { app: nginx } }
  template:
    metadata: { labels: { app: nginx } }
    spec:
      containers: [{ name: nginx, image: nginx }]
---
apiVersion: v1
kind: Service
metadata: { name: nginx }
spec:
  selector: { app: nginx }
  ports: [{ port: 80, targetPort: 80 }]
EOF

##############################################################################
# 2. Launch Satellite (background)
##############################################################################
rm -rf data && mkdir -p data

echo "🚀  starting Satellite, will capture for ${WAIT_SECS}s ..."
go run ./cmd/satellite --output-dir ./data --log-level info &
SAT_PID=$!

sleep "${WAIT_SECS}"

##############################################################################
# 3. Shutdown Satellite cleanly
##############################################################################
kill -INT "${SAT_PID}"            # mimic Ctrl-C
wait "${SAT_PID}" || true         # ignore exit status after signal

##############################################################################
# 4. Inspect newest graph snapshot
##############################################################################
LATEST=$(ls -1t data/graph-*.json | head -1)
[[ -z "${LATEST}" ]] && { echo "❌  no graph file emitted"; exit 1; }

read NODES RELS < <(
  jq -r '.nodes|length, .relationships|length' "${LATEST}"
)

echo "📊  $LATEST -> nodes=$NODES  relationships=$RELS"

##############################################################################
# 5. Assertions
##############################################################################
if (( NODES < MIN_NODES )); then
  echo "❌  expected ≥${MIN_NODES} nodes"; exit 1
fi
if (( RELS < MIN_RELS )); then
  echo "❌  expected ≥${MIN_RELS} relationships"; exit 1
fi

echo "✅  smoke test passed"

##############################################################################
# 6. Cleanup sample workload (optional)
##############################################################################
kubectl delete svc nginx deploy nginx --ignore-not-found

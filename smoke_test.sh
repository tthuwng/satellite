#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# smoke_test.sh – bullet-proof e2e sanity check for Satellite + Minikube
# ---------------------------------------------------------------------------
set -euo pipefail
shopt -s nocasematch              # for pkill pattern

##############################################################################
# Tunables
##############################################################################
PROFILE=${PROFILE:-minikube}      # `PROFILE=myprofile ./smoke_test.sh`
WAIT_SECS=${WAIT_SECS:-30}        # seconds Satellite should observe the cluster
MIN_NODES=${MIN_NODES:-10}
MIN_RELS=${MIN_RELS:-5}

##############################################################################
# 0. Kill any Satellite leftovers
##############################################################################
echo "🔪  terminating stale Satellite processes (if any)…"
pkill -f 'go run .*cmd/satellite'   || true
pkill -f './satellite'              || true
sleep 1                             # give them a moment to die

##############################################################################
# 1. Ensure cluster
##############################################################################
export MINIKUBE_IN_STYLE=false
echo "😄  $(minikube version | head -1)"

minikube status -p "$PROFILE" >/dev/null 2>&1 || \
  minikube start -p "$PROFILE" --driver=docker

kubectl config use-context "$PROFILE" >/dev/null

##############################################################################
# 2. Deploy tiny workload (idempotent)
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
# 3. Launch Satellite in a temp output dir
##############################################################################
DATA_DIR=$(mktemp -d satellite-data-XXXX)
echo "🚀  starting Satellite → ${DATA_DIR}"
go run ./cmd/satellite --output-dir "${DATA_DIR}" --log-level info &
SAT_PID=$!

# wait until at least one graph file appears or timeout
echo -n "⌛  waiting for first graph snapshot "
for ((i=0; i<WAIT_SECS; i++)); do
  if ls "${DATA_DIR}"/graph-*.json >/dev/null 2>&1; then break; fi
  sleep 1; echo -n "."
done
echo

##############################################################################
# 4. Let Satellite observe for the remainder of WAIT_SECS
##############################################################################
printf '⏳  observing cluster '
for ((j=0; j<WAIT_SECS; j++)); do
  sleep 1; printf '.'
done
printf '\n'

##############################################################################
# 5. Shutdown Satellite cleanly
##############################################################################
kill -INT "${SAT_PID}"
wait "${SAT_PID}" || true

##############################################################################
# 6. Inspect newest graph
##############################################################################
LATEST=$(ls -1t "${DATA_DIR}"/graph-*.json | head -1) || {
  echo "❌  no graph file emitted"; exit 1; }

read NODES RELS < <(
  jq -r '.nodes|length, .relationships|length' "${LATEST}")

echo "📊  ${LATEST##*/} – nodes=${NODES}  relationships=${RELS}"

##############################################################################
# 7. Assertions
##############################################################################
(( NODES >= MIN_NODES )) || { echo "❌  expected ≥${MIN_NODES} nodes"; exit 1; }
(( RELS  >= MIN_RELS  )) || { echo "❌  expected ≥${MIN_RELS} relationships"; exit 1; }

echo "✅  smoke test PASSED"

##############################################################################
# 8. Cleanup sample workload & temp dir
##############################################################################
kubectl delete svc nginx deploy nginx --ignore-not-found
rm -rf "${DATA_DIR}"

#!/bin/bash
#
# Enqueue a coding-agent job to test the squid egress proxy sidecar.
#
# The job is placed on the 'coding-agent' Faktory queue, which is consumed
# exclusively by the coding-agent worker (src/Procfile). That worker passes
# --queue=coding-agent which triggers squid sidecar injection (k8s.go:262).
# The normal 'runner' worker ignores this queue, demonstrating production-like routing.
#
# Usage: ./tests/enqueue-coding-agent-job.sh
#
# Prerequisites:
#   1. kind cluster up with helper image loaded:
#        task build-helper-image
#   2. squid-config ConfigMap applied to the default namespace:
#        kubectl apply -f - <<EOF
#        $(sed 's/namespace: runner-jobs-privileged/namespace: default/' \
#          /path/to/opslevel-kubernetes/clusters/new-dev/runner-jobs-privileged/squid-allowlist-configmap.yaml)
#        EOF
#   3. All three Procfile processes running (goreman via `task start-faktory`):
#        faktory       - Faktory server
#        runner        - normal worker, --queues=runner, no sidecar
#        coding-agent  - sidecar worker, --queues=coding-agent --queue=coding-agent
#
# After enqueue, the pod will have 2 containers (job + squid-proxy). The job
# container runs `sleep <job-pod-max-lifetime>` (default 3600s) independently
# of the job commands, so the pod stays alive for exec after the job completes.
#
# Manual proxy probe:
#   POD=$(kubectl get pods -n default -l app.kubernetes.io/managed-by=runner-faktory \
#           --sort-by=.metadata.creationTimestamp -o name | tail -1)
#
#   # Confirm squid got the PROXY_ALLOWED_DOMAINS append:
#   kubectl exec -n default $POD -c squid-proxy -- cat /etc/squid/conf.d/allowed-domains.txt
#
#   # Exec into the job container:
#   kubectl exec -it -n default $POD -c job -- sh
#   Inside:
#     export http_proxy=http://localhost:3128 https_proxy=http://localhost:3128
#     # Allowed via PROXY_ALLOWED_DOMAINS runtime append:
#     wget -qO- http://example.com  >/dev/null && echo "ALLOWED: example.com (PROXY_ALLOWED_DOMAINS)"
#     # Allowed via base allowlist:
#     wget -qO- https://github.com  >/dev/null && echo "ALLOWED: github.com (base list)"
#     # Denied (not in allowlist):
#     wget -qO- https://wikipedia.org >/dev/null && echo "OPEN" || echo "DENIED: wikipedia.org"
#     # For richer output: apk add --no-cache curl
#     # curl -x http://localhost:3128 -v https://github.com
#
#   # Check squid access log (TCP_DENIED vs allowed):
#   kubectl logs -n default $POD -c squid-proxy
#
# Cleanup stale job pods after testing:
#   kubectl delete pods -n default -l app.kubernetes.io/managed-by=runner-faktory
#

set -e

# load KUBECONFIG (.env.local) + set $cmd / KIND_EXPERIMENTAL_PROVIDER for k8s context
SCRIPT_DIR="${BASH_SOURCE[0]%/*}/../bin"
source "$SCRIPT_DIR/kind-env.sh"

src="${BASH_SOURCE[0]%/*}/../src"
JOB_ID="coding-agent-proxy-test-$(date +%s)"

echo "Enqueuing coding-agent proxy test job (ID: ${JOB_ID}) ..."

JOB_FILE=$(mktemp)
cat > "$JOB_FILE" <<ENDJOB
type: legacy
queue: coding-agent
reserve_for: 3600
retries: 0
args:
  - image: "alpine:latest"
    commands:
      - "echo Coding-agent proxy test pod up. Job ID: ${JOB_ID}"
      - "echo Squid sidecar reachable at localhost:3128"
      - "sleep 1m"
    variables:
      - key: "PROXY_ALLOWED_DOMAINS"
        value: "example.com"
        sensitive: false
    files: []
custom:
  opslevel-runner-job-id: "${JOB_ID}"
ENDJOB

go run -C "$src" . --log-level DEBUG enqueue -f "$JOB_FILE"

rm -f "$JOB_FILE"

echo ""
echo "Job enqueued (ID: ${JOB_ID}) on queue 'coding-agent'"
echo ""
echo "Watch for pod (expect containers: job + squid-proxy):"
echo "  kubectl get pods -n default -l app.kubernetes.io/managed-by=runner-faktory -o wide -w"
echo ""
echo "Latest pod:"
echo "  POD=\$(kubectl get pods -n default -l app.kubernetes.io/managed-by=runner-faktory \\"
echo "          --sort-by=.metadata.creationTimestamp -o name | tail -1)"
echo ""
echo "Monitor at: http://localhost:7420"

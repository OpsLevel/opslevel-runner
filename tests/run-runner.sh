#!/bin/bash
#
# Start a single runner in faktory mode for local end-to-end testing.
# Builds the binary first, then runs it with the given queue configuration.
#
# Usage: ./tests/run-runner.sh [FAKTORY_QUEUES]
#        FAKTORY_QUEUES  comma-separated list of Faktory queues to subscribe to
#                        defaults to "runner"
#
# Environment overrides:
#   OPSLEVEL_LOG_LEVEL              - default TRACE
#   OPSLEVEL_LOG_FORMAT             - default TEXT
#   OPSLEVEL_QUEUE                  - podConfig.Queue (gates squid sidecar injection).
#                                     When set to "coding-agent", also enables
#                                     --job-agent-mode=true and the helper image override.
#                                     Default: empty (no sidecar).
#   OPSLEVEL_JOB_POD_HELPER_IMAGE   - default localhost/opslevel-runner:dev
#   FAKTORY_URL                     - default tcp://localhost:7419
#
# Examples:
#   # Normal runner (no sidecar):
#   ./tests/run-runner.sh runner
#
#   # Coding-agent runner (squid sidecar injected into every pod):
#   OPSLEVEL_QUEUE=coding-agent ./tests/run-runner.sh coding-agent
#
# For a two-worker setup that mirrors production, prefer `task start-faktory`
# which launches both workers via src/Procfile using goreman.
#

set -e

# load KUBECONFIG (.env.local) + set $cmd / KIND_EXPERIMENTAL_PROVIDER so the
# runner targets the local kind cluster when spawning job pods
SCRIPT_DIR="${BASH_SOURCE[0]%/*}/../bin"
source "$SCRIPT_DIR/kind-env.sh"

FAKTORY_QUEUES=${1:-runner}
src="${BASH_SOURCE[0]%/*}/../src"
BINARY="$src/opslevel-runner"

echo "Building opslevel-runner ..."
go build -C "$src" -o "$BINARY" .

EXTRA_FLAGS=()

if [ -n "${OPSLEVEL_QUEUE:-}" ]; then
  EXTRA_FLAGS+=(--queue "$OPSLEVEL_QUEUE")
  if [ "$OPSLEVEL_QUEUE" = "coding-agent" ]; then
    EXTRA_FLAGS+=(--job-agent-mode=true)
  fi
fi

HELPER_IMAGE="${OPSLEVEL_JOB_POD_HELPER_IMAGE:-localhost/opslevel-runner:dev}"

echo "Starting runner (mode=faktory queues=$FAKTORY_QUEUES queue=${OPSLEVEL_QUEUE:-<none>}) ..."
exec "$BINARY" \
  --log-level "${OPSLEVEL_LOG_LEVEL:-TRACE}" \
  --log-format "${OPSLEVEL_LOG_FORMAT:-TEXT}" \
  --job-pod-helper-image "$HELPER_IMAGE" \
  --job-pod-requests-cpu "${OPSLEVEL_JOB_POD_REQUESTS_CPU:-50}" \
  --job-pod-requests-memory "${OPSLEVEL_JOB_POD_REQUESTS_MEMORY:-32}" \
  "${EXTRA_FLAGS[@]}" \
  run \
  --mode faktory \
  --queues "$FAKTORY_QUEUES" \
  --runner-pod-namespace default

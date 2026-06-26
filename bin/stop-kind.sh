#!/usr/bin/env bash

set -eu

CLUSTER_NAME="${1:-opslevel-runner}"

SCRIPT_DIR="${BASH_SOURCE[0]%/*}"
source "$SCRIPT_DIR/kind-env.sh"

# delete any running pods and stop kind cluster
if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  kubectl delete pods --all --namespace default --ignore-not-found --wait --timeout=60s 2>/dev/null || true
  if [ "$("$cmd" inspect -f '{{.State.Status}}' "${CLUSTER_NAME}-control-plane" 2>/dev/null)" = "running" ]; then
    "$cmd" stop "${CLUSTER_NAME}-control-plane"
  fi
fi

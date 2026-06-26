#!/usr/bin/env bash
# Shared kind env/runtime detection. Sourced by setup-kind.sh and stop-kind.sh.
# Caller may set SCRIPT_DIR to this script's dir (bin/); defaults to self-located.
# Sets $cmd (podman|docker) and exports KUBECONFIG.

SCRIPT_DIR="${SCRIPT_DIR:-${BASH_SOURCE[0]%/*}}"

# optional local overrides (e.g. KUBECONFIG); gitignored
[ -f "$SCRIPT_DIR/../.env.local" ] && source "$SCRIPT_DIR/../.env.local"
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

if command -v podman &>/dev/null; then
  export KIND_EXPERIMENTAL_PROVIDER=podman
  cmd=podman
else
  cmd=docker
fi

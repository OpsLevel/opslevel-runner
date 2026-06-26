#!/usr/bin/env bash
# Sourced by opslevel-runner launchers (triggered by Taskfile; started through goreman
# Procfile)
# - opslevel-runner-coding-agent
# - opslevel-runner-runner
#
# - to inherit KUBECONFIG and set the right k8s context to launch jobs
# - to start the k8s cluster and/or create it if it doesn't exist
#   - the lockfile is a transient mutex guarding the create/start critical
#     section only; it is always removed when setup completes (success or
#     failure). Cluster state is authoritative — derived from
#     `kind get clusters` / container inspect, never from lock presence.
#
# Uses 'return' inside a function so sourcing callers are not killed on exit.
set -eu

_setup_kind() {
  # --wait: losers of the lock race block until the cluster exists.
  # Pass this when the caller immediately uses the cluster (e.g. `kind load`
  # in bin/build-helper-image.sh). Worker launchers omit it — the exec'd Go
  # process tolerates the cluster appearing slightly later, so blocking is unnecessary.
  if [ "${1:-}" = "--wait" ]; then local wait=true; shift; fi

  local CLUSTER_NAME="${1:-opslevel-runner}"

  local SCRIPT_DIR="${BASH_SOURCE[0]%/*}"
  # export so the exec'd worker inherits the same kubeconfig context was pinned into;
  # also sets $cmd (podman|docker) and KIND_EXPERIMENTAL_PROVIDER
  source "$SCRIPT_DIR/kind-env.sh"

  local lockfile="${TMPDIR:-/tmp}/setup-kind-${CLUSTER_NAME}.lock"

  # de-sync concurrent workers before racing the lock
  sleep "$(( ms = RANDOM % 1200 + 200, ms / 1000 )).$(printf '%03d' "$(( ms % 1000 ))")"

  if ! ( set -C; : > "$lockfile" ) 2>/dev/null; then

    # loser: another caller owns the critical section; KUBECONFIG already exported above.
    # Without --wait, return immediately — worker launchers exec a Go
    # process that can tolerate a brief delay before the cluster is ready.
    if [ -n "${wait:-}" ]; then
      until kind get clusters | grep -q "^${CLUSTER_NAME}$"; do sleep 0.5; done
    fi
    return 0
  fi

  # winner: owns the lock; always release it when done (success or failure)
  trap 'rm -f "$lockfile" 2>/dev/null || true' ERR

  # create the cluster if it doesn't exist
  if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    kind create cluster --kubeconfig "$KUBECONFIG" --name "$CLUSTER_NAME"
  fi

  # start cluster if not running yet
  if [ "$("$cmd" inspect -f '{{.State.Status}}' "${CLUSTER_NAME}-control-plane")" != "running" ]; then
    "$cmd" start "${CLUSTER_NAME}-control-plane"
  fi

  # set context for user interaction
  kubectl config set-context "kind-${CLUSTER_NAME}" --namespace default
  kubectl config use-context "kind-${CLUSTER_NAME}"

  # release the mutex — lock is transient, not a persistent session flag
  trap - ERR
  rm -f "$lockfile"
}

_setup_kind "$@"

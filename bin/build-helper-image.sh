#!/usr/bin/env bash

# Build the runner helper image and load it into kind.
# Loads iff: we rebuilt this run OR the image is absent in the kind cluster.

set -eu

CLUSTER_NAME="${1:-opslevel-runner}"
HELPER_IMAGE="${HELPER_IMAGE:-localhost/opslevel-runner:local}"

SCRIPT_DIR="${BASH_SOURCE[0]%/*}"
source "$SCRIPT_DIR/kind-env.sh"

GOARCH="$(go env GOARCH)"
DIST_DIR="$SCRIPT_DIR/../dist"
DIST_BIN="$DIST_DIR/linux/${GOARCH}/opslevel-runner"
SRC_CHECKSUM_PREVIOUS="$DIST_DIR/linux/${GOARCH}/.build-checksum"

image_in_kind() {
  "$cmd" exec "${CLUSTER_NAME}-control-plane" ctr -n k8s.io images ls -q 2>/dev/null \
    | grep -q "$HELPER_IMAGE"
}

checksum_sources() {
  { cd "$SCRIPT_DIR/../src" && \
    find . \
      \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) \
      -type f \
      -print0 |
    LC_ALL=C sort -z |
    xargs -0 shasum -a 256
    shasum -a 256 "$SCRIPT_DIR/../Dockerfile"
  } | shasum -a 256 | cut -d' ' -f1
}

# checksum the real image inputs (binary embeds the compiled go code)
src_checksum="$(checksum_sources)"

build_image() {
  if [ ! -f "$DIST_BIN" ] || [ ! -f "$SRC_CHECKSUM_PREVIOUS" ] || [ "$(< "$SRC_CHECKSUM_PREVIOUS")" != "$src_checksum" ]; then
    mkdir -p "$DIST_DIR/linux/${GOARCH}"
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -C "$SCRIPT_DIR/../src" -o "$DIST_BIN" .
    "$cmd" build -f "$SCRIPT_DIR/../Dockerfile" \
      --build-arg "TARGETPLATFORM=linux/${GOARCH}" \
      -t "$HELPER_IMAGE" \
      "$DIST_DIR"
    printf '%s' "$src_checksum" > "$SRC_CHECKSUM_PREVIOUS"
    return 0
  fi
  return 1
}

if build_image || ! image_in_kind; then
  if [ "$cmd" = podman ]; then
    "$cmd" save "$HELPER_IMAGE" | kind load image-archive /dev/stdin --name "$CLUSTER_NAME"
  else
    kind load docker-image "$HELPER_IMAGE" --name "$CLUSTER_NAME"
  fi
fi

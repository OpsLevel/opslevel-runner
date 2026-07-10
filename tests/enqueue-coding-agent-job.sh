#!/bin/bash
#
# Enqueue a coding-agent job that just sleeps, then run curl-based proxy
# probes against the squid egress sidecar from outside the pod via
# `kubectl exec`. Prints PASS/FAIL markers and curl -v traces directly
# to this script's stdout (no dependency on runner-side log visibility).
#
# The job is placed on the 'coding-agent' Faktory queue, which is consumed
# exclusively by the coding-agent worker as per src/Procfile.
#
# Usage: ./tests/enqueue-coding-agent-job.sh
#
# Prerequisites:
#   - kind k8s cluster up and helper image loaded
#   - Faktory and opslevel-runner running from the Procfile
#        faktory       - Faktory server
#        coding-agent  - sidecar worker, --queues=coding-agent --queue=coding-agent
#
#   This can be done running 'task run' in your terminal beforehand.
#
# Flow:
#   1. Apply the squid-config ConfigMap (idempotent).
#   2. Delete any pre-existing coding-agent pods (identified by the
#      squid initContainer, which only coding-agent pods have).
#   3. Enqueue a `sleep 41` job on the coding-agent queue. Job variables
#      include http_proxy/https_proxy so curl inside the pod uses the
#      squid sidecar automatically, plus PROXY_ALLOWED_DOMAINS to seed
#      the runtime allowlist.
#   4. Wait for the new pod to become Ready (kubectl wait --timeout=45s).
#   5. kubectl exec into the job container and run curl probes against
#      three targets, printing PASS/FAIL markers.
#
# Probe targets (with PROXY_ALLOWED_DOMAINS=httpbin.org,www.amazon.ca):
#   - httpbin.org    -> ALLOW (via runtime allowlist / extra_allow ACL)
#   - www.amazon.ca  -> ALLOW (via runtime allowlist / extra_allow ACL)
#   - example.com    -> DENY  (control: not in any allowlist)
#
# After probes complete, the pod remains alive for the runner's main
# container lifetime (default 3600s) for interactive follow-up.
#
# Cleanup stale coding-agent pods manually:
#   kubectl get pods -n default -l app.kubernetes.io/managed-by=runner-faktory -o json \
#     | jq -r '
#         .items[]
#         | select(any(.spec.initContainers[]?; .name == "squid"))
#         | .metadata.name
#       ' \
#     | while IFS= read -r pod; do kubectl delete pod -n default "$pod"; done
#

set -e

# load KUBECONFIG (.env.local) + set $cmd / KIND_EXPERIMENTAL_PROVIDER for k8s context
SCRIPT_DIR="${BASH_SOURCE[0]%/*}/../bin"
source "$SCRIPT_DIR/kind-env.sh"

echo "Applying squid-config ConfigMap..."
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: squid-config
  namespace: default
  annotations:
    kubernetes.io/description: |
      Coding Agent Squid Egress Sidecar Proxy Configuration used in pods.
      allowed-domains.txt is the globally shared domain allowlist mounted
      into the squid-proxy sidecar.
data:
  squid.conf: |
    # Egress proxy for coding agent LLM sandbox
    # Allows only explicitly listed domains; denies all private/loopback ranges
    # to prevent sandbox from reaching internal cluster services via the proxy.

    http_port 3128

    # ACL: private and loopback address ranges
    acl to_private dst 10.0.0.0/8
    acl to_private dst 172.16.0.0/12
    acl to_private dst 192.168.0.0/16
    acl to_loopback dst 127.0.0.0/8
    acl to_loopback dst ::1
    acl to_cloud_metadata dst 169.254.0.0/16

    # ACL: allowed destination domains (shared + customer-specific, resolved at startup)
    acl allowed_domains dstdomain "/etc/squid/conf.d/allowed-domains.txt"
    # ACL: runtime per-job allowlist written from PROXY_ALLOWED_DOMAINS by the sidecar entrypoint
    acl extra_allow dstdomain "/srv/squid/custom-allowed-domains.conf"

    # block access to private networks.
    http_access deny to_private
    # block any pod-local services it shouldn't access
    http_access deny to_loopback
    # block cloud metadata endpoint (169.254.169.254)
    http_access deny to_cloud_metadata

    # Allow CONNECT tunnels (HTTPS) to allowed domains only
    http_access allow CONNECT allowed_domains
    http_access allow CONNECT extra_allow

    # Allow plain HTTP to allowed domains only
    http_access allow allowed_domains
    http_access allow extra_allow

    # Deny everything else
    http_access deny all

    # Logs
    access_log stdio:/dev/stdout
    cache_log stdio:/dev/stderr
    cache_store_log none

    # Disable cache, pure forward proxy usage
    cache deny all
  allowed-domains.txt: |
    # Claude API
    api.anthropic.com

    # Git providers
    github.com
    api.github.com
    gitlab.com
    bitbucket.org

    # Package registries - Node
    registry.npmjs.org
    npmjs.com
    yarnpkg.com
    registry.yarnpkg.com

    # Package registries - Python
    pypi.org
    files.pythonhosted.org
    pythonhosted.org

    # Package registries - Go
    proxy.golang.org
    sum.golang.org

    # Package registries - Ruby
    rubygems.org

    # Package registries - Rust
    crates.io
    static.crates.io
    index.crates.io

    # OS packages
    archive.ubuntu.com
    security.ubuntu.com
EOF

# Delete any pre-existing coding-agent pods so the post-enqueue pod lookup
# is unambiguous. Coding-agent pods are identified by the squid
# initContainer, which the runner only adds when queue == "coding-agent".
# jq is used because kubectl's jsonpath filter with wildcard is broken.
echo "Deleting dangling coding-agent pods..."
kubectl get pods -n default -l app.kubernetes.io/managed-by=runner-faktory -o json 2>/dev/null \
  | jq -r '
      .items[]
      | select(any(.spec.initContainers[]?; .name == "squid"))
      | .metadata.name
    ' \
  | while IFS= read -r pod; do
      kubectl delete pod -n default "$pod"
    done

src="${BASH_SOURCE[0]%/*}/../src"
JOB_ID="coding-agent-proxy-test-$(date +%s)"

echo "Enqueuing coding-agent proxy test job (ID: ${JOB_ID}) ..."

JOB_FILE=$(mktemp)
cat > "$JOB_FILE" <<ENDJOB
type: legacy
queue: coding-agent
reserve_for: 300
retries: 0
args:
  - image: "nicolaka/netshoot:latest"
    commands:
      - "sleep 41"
    variables:
      - key: "PROXY_ALLOWED_DOMAINS"
        value: "httpbin.org,www.amazon.ca"
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

# Find the newly-created coding-agent pod. The cleanup above guarantees this
# is the only coding-agent pod. Uses jq (kubectl's jsonpath wildcard filter is broken).
POD=""
for i in $(seq 1 45); do
  POD=$(kubectl get pods -n default -l app.kubernetes.io/managed-by=runner-faktory -o json 2>/dev/null \
    | jq -r '
        .items[]
        | select(any(.spec.initContainers[]?; .name == "squid"))
        | .metadata.name
      ' \
    | tail -n1)
  if [ -n "$POD" ]; then
    break
  fi
  sleep 1
done
if [ -z "$POD" ]; then
  echo "FATAL: coding-agent job pod not running!" >&2
  exit 1
fi
echo "Pod created: $POD"

echo "Waiting for the coding-agent job pod to become Ready..."
kubectl wait --for=condition=Ready -n default "pod/$POD" --timeout=45s

echo ""
echo "Testing egress proxy inside pod $POD ..."
echo ""
kubectl exec -n default -c job "$POD" -- bash -c '
probe() {
  local url="$1" expected="$2" label actual
  label=${url#*://}
  label=${label%%/*}
  echo "==================================================================="
  echo "PROBE: $label  url=$url  expected=$expected"
  echo "==================================================================="
  # -sS: hide progress meter but show errors; --max-time 15;
  # -o /dev/null: drop body; -w prints status line.
  # curl honors http_proxy/https_proxy env vars (set via job variables).
  if curl -sS --max-time 15 -o /dev/null -w "HTTP %{http_code}\n" "$url"; then
    actual=allow
  else
    actual=deny
  fi
  if [ "$actual" = "$expected" ]; then
    echo "RESULT: PASS  $label -> $actual (expected $expected)"
  else
    echo "RESULT: FAIL  $label -> $actual (expected $expected)"
  fi
  echo ""
}
probe https://httpbin.org/get  allow
probe https://www.amazon.ca/   allow
probe https://github.com/      allow
probe https://bitbucket.org/   allow
probe https://xkcd.com/2347/   deny

echo "==================================================================="
echo "HARDENING VERIFICATION: iptables must block bypasses around squid."
echo "Each check attempts a common bypass technique against xkcd.com."
echo "With iptables in place, all three should FAIL (BLOCKED = pass)."
echo "==================================================================="
echo ""

hardening_result() {
  # $1 = label, $2 = detail, $3 = "blocked" (want) or "reached" (fail)
  local label="$1" detail="$2" reached="$3"
  if [ "$reached" = "blocked" ]; then
    echo "RESULT: PASS  $label -> BLOCKED ($detail)"
  else
    echo "RESULT: FAIL  $label -> REACHED TARGET ($detail)"
    echo "        ^ iptables did not prevent this bypass"
  fi
  echo ""
}

echo "-------------------------------------------------------------------"
echo "HARDENING 1: unset proxy env vars, curl direct to xkcd.com"
echo "  (expects iptables REJECT on the direct TCP connect)"
echo "-------------------------------------------------------------------"
# Rely on curl'"'"'s exit code, not its stdout. When the OUTPUT chain REJECTs
# the packet, curl exits non-zero (typically 7 = "Failed to connect").
if env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY \
    curl -sS --max-time 15 -o /dev/null https://xkcd.com/2347/ >/dev/null 2>&1; then
  hardening_result "unset-env-curl" "curl succeeded (bypass reached target)" "reached"
else
  hardening_result "unset-env-curl" "curl exit non-zero (blocked at L4)" "blocked"
fi

echo "-------------------------------------------------------------------"
echo "HARDENING 2: bash /dev/tcp raw socket to xkcd.com:443"
echo "  (expects iptables REJECT on the direct TCP connect)"
echo "-------------------------------------------------------------------"
if timeout 10 bash -c "exec 3<>/dev/tcp/xkcd.com/443 && echo open <&3" >/dev/null 2>&1; then
  hardening_result "raw-tcp-socket" "TCP handshake succeeded on :443" "reached"
else
  hardening_result "raw-tcp-socket" "TCP handshake failed" "blocked"
fi

echo "-------------------------------------------------------------------"
echo "HARDENING 3: curl --resolve pinning a rogue IP for xkcd.com"
echo "  (expects iptables REJECT; --resolve bypasses DNS-based ACLs)"
echo "-------------------------------------------------------------------"
# 1.1.1.1 is Cloudflare public DNS; not xkcd but not a private range. If the
# packet gets out, the request reaches SOMETHING; if it doesn'"'"'t, curl fails.
if timeout 10 curl -sS --max-time 8 --resolve xkcd.com:443:1.1.1.1 \
    -o /dev/null -w "%{http_code}" https://xkcd.com/ 2>/dev/null | grep -q "^[1-5]"; then
  hardening_result "curl-resolve-rogue-ip" "reached some endpoint at 1.1.1.1:443" "reached"
else
  hardening_result "curl-resolve-rogue-ip" "connection failed" "blocked"
fi

echo "==================================================================="
echo "CAPABILITIES CHECK: main container must not have NET_ADMIN/NET_RAW"
echo "==================================================================="
if command -v capsh >/dev/null 2>&1; then
  caps=$(capsh --print 2>/dev/null | grep "Current:" || true)
  echo "  $caps"
  if echo "$caps" | grep -q "net_admin"; then
    echo "  FAIL: main container has cap_net_admin (should be dropped)"
  else
    echo "  PASS: cap_net_admin absent from main container"
  fi
  if echo "$caps" | grep -q "net_raw"; then
    echo "  FAIL: main container has cap_net_raw (should be dropped)"
  else
    echo "  PASS: cap_net_raw absent from main container"
  fi
else
  echo "  capsh not available in this image; skipping capability check."
fi

echo ""
echo "-------------------------------------------------------------------"
echo "HARDENING 4: root tries to disable the iptables rules"
echo "  (expects failure: NET_ADMIN dropped from main container)"
echo "-------------------------------------------------------------------"
if iptables -F 2>/dev/null; then
  echo "RESULT: FAIL  root was able to flush iptables (NET_ADMIN present)"
else
  echo "RESULT: PASS  iptables -F denied (NET_ADMIN dropped or unprivileged)"
fi

echo ""
echo "==================================================================="
echo "Summary:"
echo "  - Probes above show squid correctly ALLOWS/DENIES HTTP traffic."
echo "  - Hardening checks show iptables prevents the common bypasses."
echo "  - DNS-tunneling remains a documented follow-up (queries via the"
echo "    cluster resolver are still permitted)."
echo "==================================================================="
'

echo ""
echo "Probes complete."
echo ""
echo "Pod remains alive for interactive follow-up:"
echo "  kubectl exec -it -n default -c job $POD -- bash"
echo ""
echo "Squid access log (proxy-level ALLOW/DENY audit):"
echo "  kubectl logs -n default -c squid $POD"
echo ""
echo "Faktory: http://localhost:7420"

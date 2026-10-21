#!/usr/bin/env bash
# Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

#
# Fresh-machine install check. The Dockerfile installs `ao` on PATH in a clean
# image and runs this; it proves a freshly installed binary actually works on a
# machine with no Go toolchain and no developer state. The COMPREHENSIVE,
# cross-platform behavioural suite lives in Go (backend/internal/cli/e2e_test.go,
# `go test -tags e2e`); this stays deliberately small and linear.

set -euo pipefail

AO_BIN="${AO_BIN:-ao}"
tmp="$(mktemp -d)"
export AO_RUN_FILE="$tmp/running.json"
export AO_DATA_DIR="$tmp/data"
test_proxy_pid=""
cleanup() {
  if [[ -n "$test_proxy_pid" ]]; then
    kill "$test_proxy_pid" 2>/dev/null || true
    wait "$test_proxy_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

fail() { echo "FAIL: $1" >&2; exit 1; }

echo "ao binary : $(command -v "$AO_BIN")"
"$AO_BIN" version            >/dev/null || fail "version"
"$AO_BIN" doctor             >/dev/null || fail "doctor"

# Exercise the normal pinned product binary against a loopback-only CONNECT
# rejection fixture. The proxy never forwards a request to GitHub, and exposes
# no production URL override. Daemon loopback calls remain outside the proxy.
"${AO_TEST_PROXY_BIN:-ao-test-proxy}" --address-file "$tmp/proxy-address" --request-log "$tmp/proxy-requests" > "$tmp/proxy-output" 2>&1 &
test_proxy_pid=$!
for attempt in {1..100}; do
  [[ ! -s "$tmp/proxy-address" ]] || break
  kill -0 "$test_proxy_pid" 2>/dev/null || fail "test proxy exited before listening"
  sleep 0.05
done
[[ -s "$tmp/proxy-address" ]] || fail "test proxy did not listen"
if err="$(HTTPS_PROXY="http://$(cat "$tmp/proxy-address")" NO_PROXY="localhost,127.0.0.1" "$AO_BIN" start 2>&1)"; then
  fail "start unexpectedly succeeded on a fresh machine with no installed app"
fi
printf '%s\n' "$err"
echo "$err" | grep -qiE "download|ao start:" || fail "start did not fail with a clear error; got: $err"
case "$(uname -m)" in
  x86_64|amd64)
    grep -Fxq 'CONNECT github.com:443' "$tmp/proxy-requests" || fail "start did not attempt the controlled GitHub proxy"
    echo "$err" | grep -Fq 'https://github.com/grzegorzrzeznikiewicz/orchestrator-board/releases/latest/download/orchestrator-board-linux-x64.AppImage' || fail "start did not request the fork release URL"
    echo "$err" | grep -Fq 'Service Unavailable' || fail "start did not surface the controlled 503"
    cat "$tmp/proxy-requests"
    ;;
  *)
    [[ ! -s "$tmp/proxy-requests" ]] || fail "unsupported architecture unexpectedly requested a release"
    echo "$err" | grep -q 'unsupported architecture' || fail "unsupported architecture lacked a clear error"
    ;;
esac

echo "fresh-install check: OK"

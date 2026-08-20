#!/usr/bin/env bash
# Retry Exhaustion
#
# Proves that a handler that always fails is retried by the scheduler until
# attempts reach max_attempts, after which the execution is marked failed and
# the full retry arc is visible in the event log.
set -euo pipefail
cd "$(dirname "$0")"
source ./common.sh

echo "==> 1. Submit a call whose handler always fails (fail=true)"
resp=$(mcp_call "slow_compute" '{"seconds":1,"fail":true}' "retry-$(date +%s)")
EID=$(execution_id_from "$resp")
echo "    execution_id = $EID"

echo "==> 2. Watch the scheduler promote retries until attempts = max_attempts"
wait_for_status "$EID" "failed" 120
detail=$(rest_get "/api/v1/executions/${EID}")
echo "    final status = $(echo "$detail" | jq -r '.status')"
echo "    attempts     = $(echo "$detail" | jq -r '.attempts') / $(echo "$detail" | jq -r '.max_attempts')"
echo "    error        = $(echo "$detail" | jq -r '.error_message')"

echo
echo "==> Event log for $EID (full retry arc):"
print_events "$EID"

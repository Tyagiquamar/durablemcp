#!/usr/bin/env bash
# Duplicate Submission
#
# Proves that resubmitting the same (namespace, tool, idempotency_key) returns
# the original execution instead of creating a new row, and that once completed
# the cached result is returned immediately.
set -euo pipefail
cd "$(dirname "$0")"
source ./common.sh

KEY="order-abc-123"

echo "==> 1. Submit a call with idempotency_key=$KEY"
r1=$(mcp_call "call_api" '{"url":"https://example.com"}' "$KEY")
E1=$(execution_id_from "$r1")
echo "    execution_id = $E1"

echo "==> 2. Submit the same call again (simulated client retry)"
r2=$(mcp_call "call_api" '{"url":"https://example.com"}' "$KEY")
E2=$(execution_id_from "$r2")
DUP=$(echo "$r2" | jq -r '.result._meta.duplicate')
echo "    execution_id = $E2  duplicate=$DUP"

if [ "$E1" = "$E2" ]; then
  echo "    OK: same execution_id returned -- no duplicate row created"
else
  echo "    FAIL: got a different execution_id" >&2; exit 1
fi

echo "==> 3. Wait for the execution to complete"
wait_for_status "$E1" "completed" 60

echo "==> 4. Submit a third time -- expect the cached result immediately"
r3=$(mcp_call "call_api" '{"url":"https://example.com"}' "$KEY")
echo "$r3" | jq -r '"    status=\(.result._meta.status) duplicate=\(.result._meta.duplicate)"'
echo "    content: $(echo "$r3" | jq -r '.result.content[0].text' | head -c 120)"

echo
echo "==> Event log for $E1:"
print_events "$E1"

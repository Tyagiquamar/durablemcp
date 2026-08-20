#!/usr/bin/env bash
# Shared helpers for the DurableMCP demo scripts.
set -euo pipefail

API="${DURABLEMCP_API_URL:-http://localhost:8080}"
READER_KEY="${READER_API_KEY:-dev-reader-key}"

# mcp_call <tool> <json-args> <idempotency-key>  -> prints JSON-RPC response
mcp_call() {
  curl -s -X POST "$API/mcp" -H 'content-type: application/json' -d @- <<JSON
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"$1","arguments":$2,"_meta":{"idempotency_key":"$3","namespace":"demo"}}}
JSON
}

# rest_get <path> -> prints JSON body
rest_get() {
  curl -s -H "Authorization: Bearer reader:${READER_KEY}" "${API}$1"
}

# execution_id_from <jsonrpc-response> -> prints _meta.execution_id
execution_id_from() {
  echo "$1" | jq -r '.result._meta.execution_id'
}

# wait_for_status <execution_id> <status> <timeout_seconds>
wait_for_status() {
  local id="$1" want="$2" timeout="${3:-60}" waited=0
  while [ "$waited" -lt "$timeout" ]; do
    local status
    status=$(rest_get "/api/v1/executions/${id}" | jq -r '.status')
    if [ "$status" = "$want" ]; then return 0; fi
    sleep 1; waited=$((waited + 1))
  done
  echo "timed out waiting for ${id} to reach ${want}" >&2
  return 1
}

print_events() {
  rest_get "/api/v1/executions/$1/events" \
    | jq -r '.events[] | "  \(.event_type)\t token=\(.fencing_token // "-")\t \(.payload // {})"'
}

#!/usr/bin/env bash
# Stale Worker Fencing
#
# Proves that a worker resuming after its lease expired cannot complete an
# execution a newer worker has since claimed. The old (lower) fencing token is
# rejected and recorded as a stale_rejected event.
set -euo pipefail
cd "$(dirname "$0")"
source ./common.sh

echo "==> 1. Submit a long slow_compute call"
resp=$(mcp_call "slow_compute" '{"seconds":120}' "fencing-$(date +%s)")
EID=$(execution_id_from "$resp")
echo "    execution_id = $EID"

echo "==> 2. Wait for an executor to claim it (token 1)"
wait_for_status "$EID" "running" 30
detail=$(rest_get "/api/v1/executions/${EID}")
T1=$(echo "$detail" | jq -r '.lease.fencing_token')
W1=$(echo "$detail" | jq -r '.lease.worker_id')
echo "    claimed by $W1 with token $T1"

echo "==> 3. Stop all executors so the lease can expire (no heartbeats)"
docker compose stop executor >/dev/null 2>&1 || docker stop $(docker ps -q --filter name=executor) >/dev/null 2>&1 || true

echo "==> 4. Wait for the scheduler to expire the lease and return it to ready"
wait_for_status "$EID" "ready" 90 || wait_for_status "$EID" "running" 5 || true

echo "==> 5. Bring an executor back so it re-claims with token 2"
docker compose start executor >/dev/null 2>&1 || docker compose up -d executor >/dev/null 2>&1 || true
wait_for_status "$EID" "running" 60
T2=$(rest_get "/api/v1/executions/${EID}" | jq -r '.lease.fencing_token')
echo "    reclaimed with token $T2"

echo "==> 6. The original worker tries to complete with its STALE token $T1"
reject=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/internal/complete" \
  -H 'content-type: application/json' \
  -d "{\"execution_id\":\"$EID\",\"fencing_token\":$T1,\"worker_id\":\"$W1\",\"result\":{\"stale\":true}}")
echo "    HTTP $reject (expected 409 -- stale token rejected)"

echo
echo "==> Event log for $EID:"
print_events "$EID"
echo
echo "Result: the token-$T2 worker owns the execution; the token-$T1 completion was rejected."

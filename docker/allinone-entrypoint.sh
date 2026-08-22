#!/bin/sh
# All-in-one demo entrypoint: server + scheduler + two executors in a single
# container for free-tier hosts (Render/SnapDeploy). Two executors keep the
# lease-fencing demo honest: both poll the same Postgres-backed queue under
# distinct identities derived from the process list.
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"

POLL_INTERVAL_MS="${POLL_INTERVAL_MS:-500}"
LEASE_SECONDS="${LEASE_SECONDS:-30}"

/durablemcp-server &
SERVER_PID=$!
/durablemcp-scheduler &
SCHEDULER_PID=$!
WORKER_ID="${WORKER_ID:-worker-a}" /durablemcp-executor &
EXECUTOR_A_PID=$!
WORKER_ID="${WORKER_ID_B:-worker-b}" /durablemcp-executor &
EXECUTOR_B_PID=$!

# Self-driving demo data: real tool-call submissions plus deliberate
# lease-abandonment cycles so the dashboard shows live recoveries.
DEMO_AGENT_PID=""
if [ "${DEMO_AGENT:-true}" = "true" ]; then
  /durablemcp-demo-agent &
  DEMO_AGENT_PID=$!
fi

# Portable `wait -n` replacement: busybox ash does not support wait -n, so poll
# the pids directly and exit (letting the platform restart us) when any
# component dies.
PIDS="$SERVER_PID $SCHEDULER_PID $EXECUTOR_A_PID $EXECUTOR_B_PID"
if [ -n "$DEMO_AGENT_PID" ]; then
  PIDS="$PIDS $DEMO_AGENT_PID"
fi

while sleep 2; do
  for pid in $PIDS; do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "allinone: component pid $pid exited, shutting down for restart" >&2
      exit 1
    fi
  done
done

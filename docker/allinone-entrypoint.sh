#!/bin/sh
# All-in-one demo entrypoint: server + scheduler + two executors in a single
# container for free-tier hosts (Render/SnapDeploy). Two executors keep the
# lease-fencing demo honest: both poll the same Postgres-backed queue.
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"

/durablemcp-server &
/durablemcp-scheduler &
POLL_INTERVAL_MS="${POLL_INTERVAL_MS:-500}" LEASE_SECONDS="${LEASE_SECONDS:-30}" /durablemcp-executor &
POLL_INTERVAL_MS="${POLL_INTERVAL_MS:-500}" LEASE_SECONDS="${LEASE_SECONDS:-30}" /durablemcp-executor &

# Exit (and let the platform restart the container) when any component dies.
wait -n
echo "allinone: a component exited, shutting down for restart" >&2
exit 1

#!/bin/sh
# Select which DurableMCP binary to run based on DURABLEMCP_RUN.
set -e

case "${DURABLEMCP_RUN:-server}" in
  server)    exec /durablemcp-server ;;
  executor)  exec /durablemcp-executor ;;
  scheduler) exec /durablemcp-scheduler ;;
  *) echo "invalid DURABLEMCP_RUN: ${DURABLEMCP_RUN}" >&2; exit 64 ;;
esac

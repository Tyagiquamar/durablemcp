# DurableMCP

A Go MCP (Model Context Protocol) server where **every tool execution is backed
by a PostgreSQL-persisted fencing-token lease and an ordered event log.** When an
MCP client (Claude Desktop, Cursor, any agent) calls a tool, DurableMCP
guarantees the call is idempotent, that crashes mid-execution do not silently
lose state, and that every state transition is inspectable from PostgreSQL
through a read-only Next.js dashboard.

This is deliberately not a general agent framework. The one hard problem it
solves is: **what happens to an MCP tool call when the executor crashes, the
client retries, and the side effect may or may not have happened?**

## The Core Guarantee

A tool call is safe when:

1. The MCP server persists the call with a unique `execution_id` and
   `idempotency_key` **before** dispatching to an executor.
2. The executor claims the execution with a **monotonically increasing fencing
   token**: `(execution_id, token, lease_expires, worker_id)`.
3. The executor heartbeats while working. If the lease expires, the scheduler
   returns the execution to `ready` for re-claim.
4. A stale executor resuming after lease expiry is **rejected** — its fencing
   token is lower than the current claim's token.
5. Every state transition is appended to an immutable `execution_events` table.
6. Resubmitting the same `(namespace, tool_name, idempotency_key)` returns the
   original execution instead of creating a new one.

**Delivery guarantee: at-least-once.** Side-effecting tools must supply their own
idempotency key for external writes. DurableMCP's job is the engine-side state
transition, not external idempotency.

## Architecture

```
cmd/server      MCP protocol (stdio + HTTP/SSE) + read-only REST API
cmd/executor    Tool executor worker: claim, heartbeat, complete, stale-reject
cmd/scheduler   Lease reaper + retry promoter
internal/mcp    JSON-RPC 2.0 / MCP 2025-03-26 protocol + transports
internal/store  PostgreSQL repositories (raw pgx, no ORM)
internal/executor  Execution engine + demo tool handlers
internal/api    REST read API for the dashboard
web/            Next.js dashboard (Demo + Live modes)
migrations/     001_init.sql — the schema is the proof
scripts/        fencing-demo.sh, duplicate-demo.sh, retry-demo.sh
```

## Quick Start

```bash
docker compose up --build
```

- Dashboard: http://localhost:3100 (Demo mode by default; add `?mode=live`)
- REST API: http://localhost:8080/api/v1/stats
- MCP HTTP endpoint: `POST http://localhost:8080/mcp`

The stack runs Postgres (schema auto-applied on first boot via
`docker-entrypoint-initdb.d`), the MCP server, **two** executors (to demonstrate
fencing), the scheduler, and the dashboard.

### Call a tool over MCP HTTP

```bash
curl -s -X POST http://localhost:8080/mcp -H 'content-type: application/json' -d '{
  "jsonrpc":"2.0","id":1,"method":"tools/call",
  "params":{"name":"call_api","arguments":{"url":"https://example.com"},
            "_meta":{"idempotency_key":"demo-1","namespace":"demo"}}
}'
```

### Run the failure-scene demos

Requires `curl` and `jq`, with the compose stack running:

```bash
./scripts/duplicate-demo.sh   # duplicate submission returns the original execution
./scripts/retry-demo.sh       # retry until max_attempts, then fail
./scripts/fencing-demo.sh     # stale worker rejected after lease reclaim
```

## MCP Methods

`initialize`, `tools/list`, `tools/call`, `ping`. The server persists and
dispatches — it does **not** execute the tool synchronously. `tools/call`
returns an `execution_id` and status; the executor runs the work asynchronously.

### Claude Desktop config (stdio transport)

```json
{
  "mcpServers": {
    "durablemcp": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "--network", "durablemcp_default",
        "-e", "DATABASE_URL=postgres://durablemcp:durablemcp@postgres:5432/durablemcp?sslmode=disable",
        "-e", "MCP_TRANSPORT=stdio",
        "durablemcp-server"
      ]
    }
  }
}
```

## Demo Tools

The point is not what they do — their execution is governed by the fencing
contract. Pass `{"fail": true}` to any tool to force a retryable failure (used by
the retry-exhaustion demo).

| Tool | Behavior |
|------|----------|
| `slow_compute` | Sleep N seconds then return a result (simulates long work) |
| `call_api` | GET an external URL and return the body (read-only, safe to retry) |
| `write_file` | Write content under the executor scratch dir (idempotent by path+hash) |
| `send_webhook` | POST JSON to a URL (side-effecting — needs an idempotency key) |

## REST Read API

Auth: `Authorization: Bearer reader:<READER_API_KEY>` (omit when the key is unset).

```
GET /api/v1/stats
GET /api/v1/executions?status=&tool=&limit=&offset=
GET /api/v1/executions/:id
GET /api/v1/executions/:id/events
GET /api/v1/tools
GET /api/v1/workers
```

MCP endpoints are unauthenticated (assume a local/trusted network).

## Environment Variables

| Variable | Used by | Default |
|----------|---------|---------|
| `DATABASE_URL` | all | `postgres://durablemcp:durablemcp@localhost:5432/durablemcp?sslmode=disable` |
| `READER_API_KEY` | server, dashboard | _(unset — API open)_ |
| `MCP_TRANSPORT` | server | `http` (`stdio` \| `http`) |
| `LOG_LEVEL` | all | `info` |
| `ALLOW_TEST_ENDPOINTS` | server | `false` (enables `POST /internal/complete` for the fencing demo) |
| `WORKER_ID` | executor | `worker-1` |
| `POLL_INTERVAL_MS` | executor | `500` |
| `LEASE_SECONDS` | executor | `30` |
| `TICK_SECONDS` | scheduler | `5` |
| `DURABLEMCP_API_URL` | dashboard | `http://localhost:8080` |

## Dashboard: Demo vs Live

- **Demo mode** (default): deterministic fixtures covering a completed execution
  with full history, a running execution with an active lease and fencing token,
  a stale-rejected execution, a retry-exhausted failure, and a duplicate-detected
  execution. No API required.
- **Live mode** (`?mode=live`): reads from `DURABLEMCP_API_URL`. It never
  substitutes fixture data — an unreachable API shows an explicit error state.

## How This Is Tested

No CI service runs these — `make verify` is the local pre-push ritual:

```
make verify   # build + vet + full suite (boots throwaway PostgreSQL via testcontainers)
```

| Suite | What it proves |
|---|---|
| `internal/store` | The guarantee matrix against real PostgreSQL: claim serialization under concurrent racers, monotonically increasing fencing tokens, stale heartbeat/complete/fail rejection with atomic `stale_rejected` events, duplicate-submit dedupe + cached-result replay, retry-exhaustion arcs, lease-expiry recovery, terminal failure at max attempts |
| `internal/executor` | A real executor subprocess claims work and is SIGKILLed mid-heartbeat-window; the lease expires, another worker reclaims with a higher token, and the dead worker's late completion is rejected — the automated form of `scripts/fencing-demo.sh` |

## Capability Boundary

**Implemented**

- PostgreSQL persistence contract for executions, leases, fencing tokens, events
- At-least-once tool execution with stale-worker rejection
- Duplicate submission protection via idempotency key
- Retry scheduling with exponential backoff up to `max_attempts`
- Lease expiry and scheduler reaper
- REST read API for the dashboard
- MCP stdio and HTTP/SSE transports
- Demo and Live dashboard modes
- Failure-scene scripts for fencing, duplicate, and retry exhaustion
- Live hosted demo on free-tier hosting with a self-driving demo agent
	(dashboard: https://durablemcp-dashboard.vercel.app)

**Not implemented (roadmap)**

- gRPC executor transport
- OpenTelemetry spans + Prometheus metrics
- Cancellation propagation to running executors
- Tool output streaming for long-running calls

## What This Proves

> "I understand that MCP tool calls fail at the infrastructure layer — not the
> prompt layer. I built the durable execution substrate so agents can recover
> from worker crashes without silent data loss or duplicate side effects."

That is a systems insight, not an AI insight — which is what makes it non-trivial.

## Development

```bash
make build     # go build ./...
make test      # go test ./...
make run-server
make run-executor
make run-scheduler
cd web && pnpm dev   # dashboard on :3000
```

The production `Dockerfile` builds all three Go binaries into one image; the role
is selected at runtime via `DURABLEMCP_RUN` (`server` | `executor` | `scheduler`,
see `docker/railway-entrypoint.sh`). `docker-compose.yml` uses the Go dev image
with `go run` for fast local iteration.

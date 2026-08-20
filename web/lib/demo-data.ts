import type {
  DashboardData,
  Execution,
  ExecutionDetail,
  ExecutionEvent,
  Tool,
  Worker,
} from "./types"

const now = Date.parse("2026-08-20T12:00:00Z")
const iso = (offsetSeconds: number) => new Date(now + offsetSeconds * 1000).toISOString()

const demoTools: Tool[] = [
  {
    id: "tool-slow",
    name: "slow_compute",
    description: "Sleep N seconds then return a result. Simulates long-running work.",
    input_schema: { type: "object", properties: { seconds: { type: "integer" }, fail: { type: "boolean" } } },
    max_attempts: 3,
    lease_seconds: 30,
    created_at: iso(-3600),
  },
  {
    id: "tool-api",
    name: "call_api",
    description: "GET an external URL and return the response body. Read-only, safe to retry.",
    input_schema: { type: "object", properties: { url: { type: "string" } } },
    max_attempts: 3,
    lease_seconds: 30,
    created_at: iso(-3600),
  },
  {
    id: "tool-file",
    name: "write_file",
    description: "Write content to a path under the executor scratch dir. Idempotent by path+hash.",
    input_schema: { type: "object", properties: { path: { type: "string" }, content: { type: "string" } } },
    max_attempts: 3,
    lease_seconds: 30,
    created_at: iso(-3600),
  },
  {
    id: "tool-webhook",
    name: "send_webhook",
    description: "POST JSON to a URL. Side-effecting -- supply an idempotency key for external writes.",
    input_schema: { type: "object", properties: { url: { type: "string" }, body: { type: "object" } } },
    max_attempts: 3,
    lease_seconds: 30,
    created_at: iso(-3600),
  },
]

const demoExecutions: Execution[] = [
  {
    id: "exec_completed_0001",
    namespace: "demo",
    tool_name: "call_api",
    idempotency_key: "report-daily-2026-08-20",
    input_args: { url: "https://example.com/status" },
    status: "completed",
    attempts: 1,
    max_attempts: 3,
    result: { status: 200, body: "ok" },
    created_at: iso(-600),
    updated_at: iso(-590),
    lease: null,
  },
  {
    id: "exec_running_0002",
    namespace: "demo",
    tool_name: "slow_compute",
    idempotency_key: "nightly-rollup-42",
    input_args: { seconds: 120 },
    status: "running",
    attempts: 2,
    max_attempts: 3,
    created_at: iso(-120),
    updated_at: iso(-20),
    lease: {
      worker_id: "worker-2",
      fencing_token: 2,
      lease_expires: iso(25),
      claimed_at: iso(-20),
    },
  },
  {
    id: "exec_stale_0003",
    namespace: "demo",
    tool_name: "send_webhook",
    idempotency_key: "charge-order-8842",
    input_args: { url: "https://payments.internal/charge", body: { order: 8842 } },
    status: "completed",
    attempts: 2,
    max_attempts: 3,
    result: { status: 200, delivered_at: iso(-45) },
    created_at: iso(-200),
    updated_at: iso(-45),
    lease: null,
  },
  {
    id: "exec_failed_0004",
    namespace: "demo",
    tool_name: "slow_compute",
    idempotency_key: "flaky-job-7",
    input_args: { seconds: 1, fail: true },
    status: "failed",
    attempts: 3,
    max_attempts: 3,
    error_message: "forced failure (fail=true)",
    created_at: iso(-300),
    updated_at: iso(-30),
    lease: null,
  },
  {
    id: "exec_duplicate_0005",
    namespace: "demo",
    tool_name: "write_file",
    idempotency_key: "manifest-v3",
    input_args: { path: "manifest.json", content: "{}" },
    status: "ready",
    attempts: 0,
    max_attempts: 3,
    created_at: iso(-90),
    updated_at: iso(-88),
    lease: null,
  },
]

const demoWorkers: Worker[] = [
  { worker_id: "worker-2", claimed: 1, oldest_claim_age_seconds: 20 },
]

const demoEvents: Record<string, ExecutionEvent[]> = {
  exec_completed_0001: [
    { id: 1, execution_id: "exec_completed_0001", event_type: "submitted", occurred_at: iso(-600) },
    { id: 2, execution_id: "exec_completed_0001", event_type: "ready", occurred_at: iso(-600) },
    { id: 3, execution_id: "exec_completed_0001", event_type: "claimed", worker_id: "worker-1", fencing_token: 1, occurred_at: iso(-598) },
    { id: 4, execution_id: "exec_completed_0001", event_type: "heartbeat", fencing_token: 1, occurred_at: iso(-595) },
    { id: 5, execution_id: "exec_completed_0001", event_type: "completed", worker_id: "worker-1", fencing_token: 1, occurred_at: iso(-590) },
  ],
  exec_running_0002: [
    { id: 6, execution_id: "exec_running_0002", event_type: "submitted", occurred_at: iso(-120) },
    { id: 7, execution_id: "exec_running_0002", event_type: "ready", occurred_at: iso(-120) },
    { id: 8, execution_id: "exec_running_0002", event_type: "claimed", worker_id: "worker-1", fencing_token: 1, occurred_at: iso(-118) },
    { id: 9, execution_id: "exec_running_0002", event_type: "lease_expired", fencing_token: 1, occurred_at: iso(-25), payload: { attempt: 1 } },
    { id: 10, execution_id: "exec_running_0002", event_type: "claimed", worker_id: "worker-2", fencing_token: 2, occurred_at: iso(-20) },
    { id: 11, execution_id: "exec_running_0002", event_type: "heartbeat", fencing_token: 2, occurred_at: iso(-5) },
  ],
  exec_stale_0003: [
    { id: 12, execution_id: "exec_stale_0003", event_type: "submitted", occurred_at: iso(-200) },
    { id: 13, execution_id: "exec_stale_0003", event_type: "ready", occurred_at: iso(-200) },
    { id: 14, execution_id: "exec_stale_0003", event_type: "claimed", worker_id: "worker-1", fencing_token: 1, occurred_at: iso(-198) },
    { id: 15, execution_id: "exec_stale_0003", event_type: "lease_expired", fencing_token: 1, occurred_at: iso(-120) },
    { id: 16, execution_id: "exec_stale_0003", event_type: "claimed", worker_id: "worker-2", fencing_token: 2, occurred_at: iso(-115) },
    { id: 17, execution_id: "exec_stale_0003", event_type: "stale_rejected", worker_id: "worker-1", fencing_token: 1, occurred_at: iso(-110), payload: { reason: "completion rejected: fencing token superseded" } },
    { id: 18, execution_id: "exec_stale_0003", event_type: "completed", worker_id: "worker-2", fencing_token: 2, occurred_at: iso(-45) },
  ],
  exec_failed_0004: [
    { id: 19, execution_id: "exec_failed_0004", event_type: "submitted", occurred_at: iso(-300) },
    { id: 20, execution_id: "exec_failed_0004", event_type: "ready", occurred_at: iso(-300) },
    { id: 21, execution_id: "exec_failed_0004", event_type: "claimed", worker_id: "worker-1", fencing_token: 1, occurred_at: iso(-298) },
    { id: 22, execution_id: "exec_failed_0004", event_type: "retry_scheduled", fencing_token: 1, occurred_at: iso(-296), payload: { attempt: 1, backoff_seconds: 2 } },
    { id: 23, execution_id: "exec_failed_0004", event_type: "claimed", worker_id: "worker-2", fencing_token: 2, occurred_at: iso(-290) },
    { id: 24, execution_id: "exec_failed_0004", event_type: "retry_scheduled", fencing_token: 2, occurred_at: iso(-288), payload: { attempt: 2, backoff_seconds: 4 } },
    { id: 25, execution_id: "exec_failed_0004", event_type: "claimed", worker_id: "worker-1", fencing_token: 3, occurred_at: iso(-282) },
    { id: 26, execution_id: "exec_failed_0004", event_type: "failed", fencing_token: 3, occurred_at: iso(-30), payload: { error: "forced failure (fail=true)", attempt: 3 } },
  ],
  exec_duplicate_0005: [
    { id: 27, execution_id: "exec_duplicate_0005", event_type: "submitted", occurred_at: iso(-90) },
    { id: 28, execution_id: "exec_duplicate_0005", event_type: "ready", occurred_at: iso(-90) },
    { id: 29, execution_id: "exec_duplicate_0005", event_type: "duplicate_detected", occurred_at: iso(-70), payload: { idempotency_key: "manifest-v3" } },
  ],
}

export function demoDashboard(): DashboardData {
  return {
    mode: "demo",
    state: "ok",
    stats: {
      total: demoExecutions.length,
      ready: demoExecutions.filter((e) => e.status === "ready").length,
      running: demoExecutions.filter((e) => e.status === "running").length,
      completed: demoExecutions.filter((e) => e.status === "completed").length,
      failed: demoExecutions.filter((e) => e.status === "failed").length,
      active_leases: demoWorkers.reduce((n, w) => n + w.claimed, 0),
    },
    executions: demoExecutions,
    tools: demoTools,
    workers: demoWorkers,
  }
}

export function demoDetail(id: string): ExecutionDetail {
  const execution = demoExecutions.find((e) => e.id === id) ?? null
  return {
    mode: "demo",
    state: execution ? "ok" : "empty",
    execution,
    events: demoEvents[id] ?? [],
  }
}

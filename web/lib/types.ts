export type DashboardMode = "demo" | "live"

export type ExecutionStatus =
  | "pending"
  | "ready"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"

export interface Lease {
  worker_id: string
  fencing_token: number
  lease_expires: string
  claimed_at: string
}

export interface Execution {
  id: string
  namespace: string
  tool_name: string
  idempotency_key: string
  input_args: unknown
  status: ExecutionStatus
  attempts: number
  max_attempts: number
  result?: unknown
  error_message?: string
  created_at: string
  updated_at: string
  lease?: Lease | null
}

export interface ExecutionEvent {
  id: number
  execution_id: string
  event_type: string
  worker_id?: string
  fencing_token?: number | null
  payload?: unknown
  occurred_at: string
}

export interface Tool {
  id: string
  name: string
  description: string
  input_schema: unknown
  max_attempts: number
  lease_seconds: number
  created_at: string
}

export interface Worker {
  worker_id: string
  claimed: number
  oldest_claim_age_seconds: number
}

export interface Stats {
  total: number
  ready: number
  running: number
  completed: number
  failed: number
  active_leases: number
}

export type DashboardState = "ok" | "empty" | "unavailable"

export interface DashboardData {
  mode: DashboardMode
  state: DashboardState
  message?: string
  stats: Stats
  executions: Execution[]
  tools: Tool[]
  workers: Worker[]
}

export interface ExecutionDetail {
  mode: DashboardMode
  state: DashboardState
  message?: string
  execution: Execution | null
  events: ExecutionEvent[]
}

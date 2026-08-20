import type {
  DashboardData,
  DashboardMode,
  Execution,
  ExecutionDetail,
  ExecutionEvent,
  Stats,
  Tool,
  Worker,
} from "./types"
import { demoDashboard, demoDetail } from "./demo-data"

export function resolveMode(mode?: string | string[]): DashboardMode {
  return (Array.isArray(mode) ? mode[0] : mode) === "live" ? "live" : "demo"
}

function apiBaseUrl(): string {
  return process.env.DURABLEMCP_API_URL ?? "http://localhost:8080"
}

function readerAuth(): Record<string, string> {
  const key = process.env.READER_API_KEY
  return key ? { Authorization: `Bearer reader:${key}` } : {}
}

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${apiBaseUrl()}${path}`, {
    cache: "no-store",
    headers: readerAuth(),
    signal: AbortSignal.timeout(5_000),
  })
  if (!res.ok) {
    throw new Error(`GET ${path} -> ${res.status}`)
  }
  return (await res.json()) as T
}

// getDashboardData never substitutes fixture data in live mode -- it surfaces
// an explicit unavailable state instead.
export async function getDashboardData(mode: DashboardMode): Promise<DashboardData> {
  if (mode === "demo") return demoDashboard()

  try {
    const [stats, execs, tools, workers] = await Promise.all([
      fetchJSON<Stats>("/api/v1/stats"),
      fetchJSON<{ executions: Execution[] }>("/api/v1/executions?limit=50"),
      fetchJSON<{ tools: Tool[] }>("/api/v1/tools"),
      fetchJSON<{ workers: Worker[] }>("/api/v1/workers"),
    ])
    const executions = execs.executions ?? []
    return {
      mode: "live",
      state: executions.length === 0 ? "empty" : "ok",
      stats,
      executions,
      tools: tools.tools ?? [],
      workers: workers.workers ?? [],
    }
  } catch (err) {
    return {
      mode: "live",
      state: "unavailable",
      message: err instanceof Error ? err.message : "API unreachable",
      stats: { total: 0, ready: 0, running: 0, completed: 0, failed: 0, active_leases: 0 },
      executions: [],
      tools: [],
      workers: [],
    }
  }
}

export async function getExecutionDetail(mode: DashboardMode, id: string): Promise<ExecutionDetail> {
  if (mode === "demo") return demoDetail(id)

  try {
    const [execution, events] = await Promise.all([
      fetchJSON<Execution>(`/api/v1/executions/${id}`),
      fetchJSON<{ events: ExecutionEvent[] }>(`/api/v1/executions/${id}/events`),
    ])
    return { mode: "live", state: "ok", execution, events: events.events ?? [] }
  } catch (err) {
    return {
      mode: "live",
      state: "unavailable",
      message: err instanceof Error ? err.message : "API unreachable",
      execution: null,
      events: [],
    }
  }
}

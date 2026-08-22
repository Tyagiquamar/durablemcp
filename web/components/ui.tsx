import type { DashboardMode, ExecutionEvent } from "@/lib/types"

export function StatusBadge({ status }: { status: string }) {
  return <span className={`badge ${status}`}>{status}</span>
}

export function Unavailable({ message }: { message?: string }) {
  return (
    <div className="notice error">
      <strong>Still waking the engine.</strong> The hosted instance runs on a free tier and sleeps when idle — the
      first request can take up to a minute, and this page retries for ~45s before giving up. It keeps retrying in
      the background; give it a moment.
      <div className="muted" style={{ marginTop: 6 }}>
        Last error: {message ?? "API unreachable"}. Live mode never falls back to demo fixtures.
      </div>
    </div>
  )
}

export function EmptyNotice() {
  return (
    <div className="notice">
      No executions yet. Call a tool via <span className="mono">POST /mcp</span> or run a demo script.
    </div>
  )
}

export function DemoNotice({ mode }: { mode: DashboardMode }) {
  if (mode !== "demo") return null
  return (
    <div className="notice">
      Deterministic fixtures. Switch to <strong>Live</strong> to watch the real engine — recoveries included.
    </div>
  )
}

export function relativeTime(iso: string): string {
  const then = Date.parse(iso)
  if (Number.isNaN(then)) return iso
  const diff = Math.round((Date.now() - then) / 1000)
  const abs = Math.abs(diff)
  if (abs < 60) return `${diff}s ago`
  if (abs < 3600) return `${Math.round(diff / 60)}m ago`
  if (abs < 86400) return `${Math.round(diff / 3600)}h ago`
  return new Date(then).toLocaleString()
}

export function EventTimeline({ events }: { events: ExecutionEvent[] }) {
  if (events.length === 0) return <p className="muted">No events recorded.</p>
  return (
    <ul className="timeline">
      {events.map((e) => (
        <li key={e.id} className={e.event_type}>
          <span className="mono muted">{new Date(e.occurred_at).toLocaleTimeString()}</span>
          <span>
            <span className="etype">{e.event_type}</span>
            {e.worker_id ? <span className="muted"> · {e.worker_id}</span> : null}
            {e.fencing_token != null ? <span className="token"> · token {e.fencing_token}</span> : null}
            {e.payload && Object.keys(e.payload as object).length > 0 ? (
              <span className="muted"> · {JSON.stringify(e.payload)}</span>
            ) : null}
          </span>
        </li>
      ))}
    </ul>
  )
}

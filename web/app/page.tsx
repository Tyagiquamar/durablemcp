import Link from "next/link"
import { getDashboardData, resolveMode } from "@/lib/dashboard-data"
import { TopBar } from "@/components/top-bar"
import { DemoNotice, EmptyNotice, StatusBadge, Unavailable, relativeTime } from "@/components/ui"

export const dynamic = "force-dynamic"

export default async function OverviewPage({
  searchParams,
}: {
  searchParams: Promise<{ mode?: string }>
}) {
  const mode = resolveMode((await searchParams).mode)
  const data = await getDashboardData(mode)

  const metrics = [
    { label: "Total", value: data.stats.total },
    { label: "Running", value: data.stats.running },
    { label: "Ready", value: data.stats.ready },
    { label: "Completed", value: data.stats.completed },
    { label: "Failed", value: data.stats.failed },
    { label: "Active leases", value: data.stats.active_leases },
  ]

  return (
    <>
      <TopBar mode={mode} active="/" />
      <div className="page-heading">
        <h1>Execution Overview</h1>
        <p>Every MCP tool call is a persisted execution governed by a fencing-token lease.</p>
      </div>

      {data.state === "unavailable" ? (
        <Unavailable message={data.message} />
      ) : (
        <>
          <DemoNotice mode={mode} />
          <div className="metric-grid">
            {metrics.map((m) => (
              <div key={m.label} className="metric">
                <div className="value">{m.value}</div>
                <div className="label">{m.label}</div>
              </div>
            ))}
          </div>

          <section className="panel">
            <h2>Recent Executions</h2>
            {data.executions.length === 0 ? (
              <EmptyNotice />
            ) : (
              <table>
                <thead>
                  <tr>
                    <th>Execution</th>
                    <th>Tool</th>
                    <th>Status</th>
                    <th>Attempts</th>
                    <th>Lease / Token</th>
                    <th>Updated</th>
                  </tr>
                </thead>
                <tbody>
                  {data.executions.map((e) => (
                    <tr key={e.id}>
                      <td className="mono">
                        <Link href={mode === "live" ? `/executions/${e.id}?mode=live` : `/executions/${e.id}`}>
                          {e.id}
                        </Link>
                        <div className="muted">{e.namespace} · {e.idempotency_key}</div>
                      </td>
                      <td className="mono">{e.tool_name}</td>
                      <td>
                        <StatusBadge status={e.status} />
                      </td>
                      <td>
                        {e.attempts}/{e.max_attempts}
                      </td>
                      <td>
                        {e.lease ? (
                          <span>
                            {e.lease.worker_id} <span className="token">token {e.lease.fencing_token}</span>
                          </span>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                      <td className="muted">{relativeTime(e.updated_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        </>
      )}
    </>
  )
}

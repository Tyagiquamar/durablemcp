import { getDashboardData, resolveMode } from "@/lib/dashboard-data"
import { SiteFooter, TopBar } from "@/components/top-bar"
import { AutoRefresh } from "@/components/auto-refresh"
import { Unavailable } from "@/components/ui"

export const dynamic = "force-dynamic"
export const maxDuration = 60

export default async function WorkersPage({
  searchParams,
}: {
  searchParams: Promise<{ mode?: string }>
}) {
  const mode = resolveMode((await searchParams).mode)
  const data = await getDashboardData(mode)

  return (
    <>
      <TopBar mode={mode} active="/workers" />
      <div className="page-heading">
        <div>
          <p className="kicker">Lease holders and fencing tokens</p>
          <h1>Workers</h1>
          <p>Derived from active leases — each worker holds executions under a fencing token.</p>
        </div>
        {mode === "live" ? <AutoRefresh /> : null}
      </div>

      {data.state === "unavailable" ? (
        <Unavailable message={data.message} />
      ) : (
        <section className="panel">
          {data.workers.length === 0 ? (
            <p className="muted">No workers currently hold a lease.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Worker</th>
                  <th>Claimed executions</th>
                  <th>Oldest claim age</th>
                </tr>
              </thead>
              <tbody>
                {data.workers.map((w) => (
                  <tr key={w.worker_id}>
                    <td className="mono">{w.worker_id}</td>
                    <td>{w.claimed}</td>
                    <td>{Math.round(w.oldest_claim_age_seconds)}s</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
      <SiteFooter />
    </>
  )
}

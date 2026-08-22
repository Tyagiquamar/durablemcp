import { getDashboardData, resolveMode } from "@/lib/dashboard-data"
import { SiteFooter, TopBar } from "@/components/top-bar"
import { AutoRefresh } from "@/components/auto-refresh"
import { Unavailable } from "@/components/ui"

export const dynamic = "force-dynamic"
export const maxDuration = 60

export default async function ToolsPage({
  searchParams,
}: {
  searchParams: Promise<{ mode?: string }>
}) {
  const mode = resolveMode((await searchParams).mode)
  const data = await getDashboardData(mode)

  return (
    <>
      <TopBar mode={mode} active="/tools" />
      <div className="page-heading">
        <div>
          <p className="kicker">Lease and retry policy per tool</p>
          <h1>Registered Tools</h1>
          <p>Tools this MCP server exposes, with their retry and lease policy.</p>
        </div>
        {mode === "live" ? <AutoRefresh intervalMs={30_000} /> : null}
      </div>

      {data.state === "unavailable" ? (
        <Unavailable message={data.message} />
      ) : (
        <section className="panel">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Description</th>
                <th>Max attempts</th>
                <th>Lease (s)</th>
              </tr>
            </thead>
            <tbody>
              {data.tools.map((t) => (
                <tr key={t.id}>
                  <td className="mono">{t.name}</td>
                  <td>{t.description}</td>
                  <td>{t.max_attempts}</td>
                  <td>{t.lease_seconds}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
      <SiteFooter />
    </>
  )
}

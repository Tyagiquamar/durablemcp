import Link from "next/link"
import { getExecutionDetail, resolveMode } from "@/lib/dashboard-data"
import { SiteFooter, TopBar } from "@/components/top-bar"
import { AutoRefresh } from "@/components/auto-refresh"
import { EventTimeline, StatusBadge, Unavailable, relativeTime } from "@/components/ui"

export const dynamic = "force-dynamic"
export const maxDuration = 60

export default async function ExecutionDetailPage({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>
  searchParams: Promise<{ mode?: string }>
}) {
  const { id } = await params
  const mode = resolveMode((await searchParams).mode)
  const detail = await getExecutionDetail(mode, id)

  return (
    <>
      <TopBar mode={mode} active="/" />
      <div className="page-heading">
        <div>
          <p className="kicker">
            <Link href={mode === "live" ? "/" : "/?mode=demo"} className="quiet-link">
              ← Back to overview
            </Link>
          </p>
          <h1>Execution Detail</h1>
        </div>
        {mode === "live" && detail.state === "ok" ? <AutoRefresh intervalMs={15_000} /> : null}
      </div>

      {detail.state === "unavailable" ? (
        <Unavailable message={detail.message} />
      ) : !detail.execution ? (
        <div className="notice">Execution not found.</div>
      ) : (
        <>
          <section className="panel">
            <h2>
              <span className="mono">{detail.execution.id}</span>{" "}
              <StatusBadge status={detail.execution.status} />
            </h2>
            <dl className="kv">
              <dt>Namespace</dt>
              <dd className="mono">{detail.execution.namespace}</dd>
              <dt>Tool</dt>
              <dd className="mono">{detail.execution.tool_name}</dd>
              <dt>Idempotency key</dt>
              <dd className="mono">{detail.execution.idempotency_key}</dd>
              <dt>Attempts</dt>
              <dd>
                {detail.execution.attempts} / {detail.execution.max_attempts}
              </dd>
              <dt>Created</dt>
              <dd>{relativeTime(detail.execution.created_at)}</dd>
              <dt>Updated</dt>
              <dd>{relativeTime(detail.execution.updated_at)}</dd>
              {detail.execution.error_message ? (
                <>
                  <dt>Error</dt>
                  <dd style={{ color: "var(--red)" }}>{detail.execution.error_message}</dd>
                </>
              ) : null}
            </dl>
          </section>

          {detail.execution.lease ? (
            <section className="panel">
              <h2>Active Lease</h2>
              <dl className="kv">
                <dt>Worker</dt>
                <dd className="mono">{detail.execution.lease.worker_id}</dd>
                <dt>Fencing token</dt>
                <dd className="token">{detail.execution.lease.fencing_token}</dd>
                <dt>Expires</dt>
                <dd>{relativeTime(detail.execution.lease.lease_expires)}</dd>
              </dl>
            </section>
          ) : null}

          <section className="panel">
            <h2>Input Args</h2>
            <pre>{JSON.stringify(detail.execution.input_args, null, 2)}</pre>
          </section>

          {detail.execution.result != null ? (
            <section className="panel">
              <h2>Result</h2>
              <pre>{JSON.stringify(detail.execution.result, null, 2)}</pre>
            </section>
          ) : null}

          <section className="panel">
            <h2>Event Timeline</h2>
            <EventTimeline events={detail.events} />
          </section>
        </>
      )}
      <SiteFooter />
    </>
  )
}

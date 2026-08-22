const points = [
  {
    kicker: "The problem",
    title: "Tool calls die halfway. Retries make it worse.",
    body: `An agent asks your API to send an email or charge an order. Your worker crashes mid-call; the client retries; another worker picks the job up. Did the email send twice? Naive queues cannot answer this, and most demos never ask.`,
  },
  {
    kicker: "The guarantee",
    title: "Persist before dispatch, fence every claim.",
    body: `Every call is written to Postgres with an idempotency key before anything runs. Each claim carries a monotonically increasing fencing token: a worker that lost its lease may still finish, but its completion is rejected atomically and recorded as a stale_rejected audit event. The honest contract is at-least-once delivery plus application idempotency — never claimed exactly-once.`,
  },
  {
    kicker: "The proof",
    title: "Not screenshots. Recoveries you can watch.",
    body: `Integration tests SIGKILL a real executor subprocess mid-heartbeat and assert the heir worker reclaims with a higher token while the ghost's replay gets rejected through the public API. The live demo goes further: an in-container agent submits genuine tool calls and periodically abandons its own short leases so this dashboard accumulates real lease_expired and stale_rejected stories.`,
  },
]

export function WhySection() {
  return (
    <section className="why">
      <p className="kicker">Why this exists</p>
      <h2 className="why-title">Durable execution for tool calls that outlive their workers</h2>
      <div className="why-grid">
        {points.map((p) => (
          <article key={p.kicker}>
            <p className="kicker">{p.kicker}</p>
            <h3>{p.title}</h3>
            <p>{p.body}</p>
          </article>
        ))}
      </div>
    </section>
  )
}

export default function Loading() {
  return (
    <div className="waking">
      <p className="kicker">Connecting to the live engine</p>
      <h1>Waking the instance…</h1>
      <p>
        The hosted engine runs on a free tier and sleeps when idle. The first request can take up to a minute — this
        page retries automatically and never shows fake data while it waits.
      </p>
      <div className="loading-lines" aria-hidden="true">
        <i />
        <i />
        <i />
      </div>
    </div>
  )
}

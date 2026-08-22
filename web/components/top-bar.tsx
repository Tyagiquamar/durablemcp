import Link from "next/link"
import type { DashboardMode } from "@/lib/types"

const links = [
  { href: "/", label: "Overview" },
  { href: "/tools", label: "Tools" },
  { href: "/workers", label: "Workers" },
]

// Live is the default; demo fixtures are opt-in via ?mode=demo.
function withMode(href: string, mode: DashboardMode): string {
  return mode === "demo" ? `${href}?mode=demo` : href
}

export function TopBar({ mode, active }: { mode: DashboardMode; active: string }) {
  return (
    <header className="masthead">
      <div className="brand">
        <p className="kicker">Durable execution · MCP tool calls</p>
        <Link href={withMode("/", mode)} className="brand-name">
          DurableMCP
        </Link>
      </div>
      <nav className="nav" aria-label="Sections">
        {links.map((l) => (
          <Link key={l.href} href={withMode(l.href, mode)} className={active === l.href ? "active" : ""}>
            {l.label}
          </Link>
        ))}
      </nav>
      <div className="mode-toggle" aria-label="Data source">
        <Link href="?mode=demo" className={mode === "demo" ? "active" : ""}>
          Demo
        </Link>
        <Link href="?" className={mode === "live" ? "active" : ""}>
          Live
        </Link>
      </div>
    </header>
  )
}

export function SiteFooter() {
  return (
    <footer className="site-footer">
      <p>
        Part of a trio of durable-execution systems —{" "}
        <a href="https://github.com/Tyagiquamar/durablemcp">source</a> ·{" "}
        <a href="https://quamar.vercel.app">portfolio</a>. Every guarantee on this page is reproducible from the repo:
        run <span className="mono">make verify</span>.
      </p>
    </footer>
  )
}

import Link from "next/link"
import type { DashboardMode } from "@/lib/types"

const links = [
  { href: "/", label: "Overview" },
  { href: "/tools", label: "Tools" },
  { href: "/workers", label: "Workers" },
]

// withMode preserves the demo/live selection across navigation.
function withMode(href: string, mode: DashboardMode): string {
  return mode === "live" ? `${href}?mode=live` : href
}

export function TopBar({ mode, active }: { mode: DashboardMode; active: string }) {
  return (
    <div className="topbar">
      <div className="brand">
        <strong>DurableMCP</strong>
        <span>Durable execution for MCP tool calls</span>
      </div>
      <nav className="nav">
        {links.map((l) => (
          <Link key={l.href} href={withMode(l.href, mode)} className={active === l.href ? "active" : ""}>
            {l.label}
          </Link>
        ))}
      </nav>
      <div className="mode-toggle">
        <Link href="?" className={mode === "demo" ? "active" : ""}>
          Demo
        </Link>
        <Link href="?mode=live" className={mode === "live" ? "active" : ""}>
          Live
        </Link>
      </div>
    </div>
  )
}

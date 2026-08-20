import type { Metadata } from "next"
import "./styles.css"

export const metadata: Metadata = {
  title: "DurableMCP Dashboard",
  description: "Durable execution substrate for MCP tool calls — leases, fencing tokens, and an immutable event log.",
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <div className="shell">{children}</div>
      </body>
    </html>
  )
}

import type { Metadata } from "next"
import { Geist, Geist_Mono, Libre_Baskerville } from "next/font/google"
import "./styles.css"

const geistSans = Geist({
  subsets: ["latin"],
  variable: "--font-sans",
})

const geistMono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
})

const libreBaskerville = Libre_Baskerville({
  subsets: ["latin"],
  weight: ["400", "700"],
  variable: "--font-display",
})

export const metadata: Metadata = {
  title: "DurableMCP — Durable Execution for MCP Tool Calls",
  description:
    "Every MCP tool call is persisted before dispatch and executed under a fencing-token lease. Watch real crash recoveries on a live dashboard.",
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${geistSans.variable} ${geistMono.variable} ${libreBaskerville.variable}`}>
      <body>
        <div className="shell">{children}</div>
      </body>
    </html>
  )
}

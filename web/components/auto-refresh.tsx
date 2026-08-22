"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"

export function AutoRefresh({ intervalMs = 20_000 }: { intervalMs?: number }) {
  const router = useRouter()
  const [lastTick, setLastTick] = useState<Date | null>(null)

  useEffect(() => {
    const timer = setInterval(() => {
      router.refresh()
      setLastTick(new Date())
    }, intervalMs)
    return () => clearInterval(timer)
  }, [router, intervalMs])

  return (
    <div className="autorefresh" role="status">
      <span className="pulse-dot" aria-hidden="true" />
      <span>
        auto-refreshes every {Math.round(intervalMs / 1000)}s
        {lastTick ? ` · last ${lastTick.toLocaleTimeString()}` : ""}
      </span>
    </div>
  )
}

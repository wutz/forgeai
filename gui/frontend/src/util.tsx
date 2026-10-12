export function errText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return JSON.stringify(e)
}

export function Status({phase}: {phase: string}) {
  return (
    <span className={`status status-${phase.toLowerCase()}`}>
      <span className="dot" aria-hidden/>{phase}
    </span>
  )
}

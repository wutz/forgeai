export function errText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  const any = e as { message?: string }
  return any?.message || JSON.stringify(e)
}

export function Status({phase}: {phase: string}) {
  return (
    <span className={`status status-${phase.toLowerCase()}`}>
      <span className="dot" aria-hidden/>{phase}
    </span>
  )
}

export type EnvMap = Record<string, string>

export function envToLines(env: EnvMap): string {
  return Object.keys(env).sort().map(k => `${k}=${env[k]}`).join('\n')
}

export function linesToEnv(s: string): EnvMap {
  const env: EnvMap = {}
  for (const line of s.split('\n')) {
    const i = line.indexOf('=')
    const name = (i < 0 ? line : line.slice(0, i)).trim()
    if (!name || name.startsWith('#')) continue
    env[name] = i < 0 ? '' : line.slice(i + 1)
  }
  return env
}

export function fmtDate(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return isNaN(+d) ? iso : d.toLocaleString()
}

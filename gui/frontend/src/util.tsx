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

// envMap turns the name/value list of a form into an EnvMap.
export function envMap(env: {name: string, value: string}[] = []): EnvMap {
  return Object.fromEntries(env.map(e => [e.name, e.value]))
}

export function fmtDate(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return isNaN(+d) ? iso : d.toLocaleString()
}

// toGiB reads a storage quantity ("500Gi", "1Ti", "2G") as GiB.
export function toGiB(q?: string): number {
  if (!q) return 0
  const m = /^([0-9.]+)\s*([KMGTPE]i?)?$/.exec(q.trim())
  if (!m) return 0
  const n = parseFloat(m[1])
  const mult: Record<string, number> = {
    Ki: 1 / 1024 ** 2, Mi: 1 / 1024, Gi: 1, Ti: 1024, Pi: 1024 ** 2,
    k: 1e-6, M: 1e-3, G: 1, T: 1e3, P: 1e6,
  }
  return n * (mult[m[2] || 'Gi'] ?? 1)
}

// pct returns value/max as a whole percentage, clamped to 0..100.
export function pct(value: number, max: number): number {
  if (!(max > 0)) return 0
  return Math.min(100, Math.max(0, Math.round((value / max) * 100)))
}

import {MessageKey, t} from './i18n'

export function errText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  const any = e as { message?: string }
  return any?.message || JSON.stringify(e)
}

const phaseKeys = new Map<string, MessageKey>([
  ['running', 'phase.running'], ['queued', 'phase.queued'], ['pending', 'phase.pending'],
  ['starting', 'phase.starting'], ['restarting', 'phase.restarting'], ['stopping', 'phase.stopping'],
  ['stopped', 'phase.stopped'], ['suspended', 'phase.suspended'], ['succeeded', 'phase.succeeded'],
  ['failed', 'phase.failed'], ['idle', 'phase.idle'], ['ready', 'phase.ready'],
  ['resizing', 'phase.resizing'],
])

// phaseLabel is the text shown for a backend phase. Code compares phases by
// their English names, so only the displayed text is translated; an unknown
// phase is shown as it arrives.
export function phaseLabel(phase: string): string {
  const key = phaseKeys.get(phase.toLowerCase())
  return key ? t(key) : phase
}

export function Status({phase}: {phase: string}) {
  return (
    <span className={`status status-${phase.toLowerCase()}`}>
      <span className="dot" aria-hidden/>{phaseLabel(phase)}
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

// k8sNameAlphabet is the alphabet of Kubernetes' generated name suffixes: no
// vowels, so a random name never spells a word, and no look-alike characters.
const k8sNameAlphabet = 'bcdfghjklmnpqrstvwxz2456789'

// generateName returns a Kubernetes-style name, prefix plus five random
// characters ("sandbox-7xk2m"), a lowercase DNS label as the API server expects.
export function generateName(prefix: string): string {
  const random = new Uint32Array(5)
  crypto.getRandomValues(random)
  const suffix = Array.from(random, n => k8sNameAlphabet[n % k8sNameAlphabet.length]).join('')
  return `${prefix}-${suffix}`
}

export function fmtDate(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return isNaN(+d) ? iso : d.toLocaleString()
}

// toGiB reads a Kubernetes storage quantity ("500Gi", "1Ti", "2G", "1000") as
// GiB. Binary suffixes (Ki..Ei) are powers of 1024, decimal ones (k..E) powers
// of 1000, and a bare number counts bytes, as in Kubernetes.
export function toGiB(q?: string): number {
  if (!q) return 0
  const m = /^([0-9.]+)\s*(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$/.exec(q.trim())
  if (!m) return 0
  const bytes: Record<string, number> = {
    '': 1,
    Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4, Pi: 1024 ** 5, Ei: 1024 ** 6,
    k: 1e3, M: 1e6, G: 1e9, T: 1e12, P: 1e15, E: 1e18,
  }
  return parseFloat(m[1]) * bytes[m[2] ?? ''] / 1024 ** 3
}

// pct returns value/max as a whole percentage, clamped to 0..100.
export function pct(value: number, max: number): number {
  if (!(max > 0)) return 0
  return Math.min(100, Math.max(0, Math.round((value / max) * 100)))
}

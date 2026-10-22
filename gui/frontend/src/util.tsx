import {MessageKey, t} from './i18n'

// knownErrors maps the backend messages users hit most to catalogue keys, so
// they read well in both languages. Other backend text is shown as written.
const knownErrors: [RegExp, MessageKey][] = [
  [/not connected: select a kubeconfig context/, 'error.notConnected'],
  [/is forbidden: /, 'error.forbidden'],
  [/has been modified; please apply your changes/, 'error.conflict'],
  [/"[^"]*" already exists/, 'error.alreadyExists'],
  [/context deadline exceeded|Client\.Timeout exceeded/, 'error.timeout'],
  [/was not created by forgeai and cannot be (edited|deleted) here/, 'error.notCreatedByForgeai'],
  [/registry is not configured for this project/, 'error.registryNotConfigured'],
  [/was superseded by a newer connect/, 'error.connectionSuperseded'],
  [/has no pod: it is stopped or still starting/, 'error.sandboxNoPod'],
]

export function errText(e: unknown): string {
  const raw = typeof e === 'string' ? e
    : e instanceof Error ? e.message
    : (e as { message?: string })?.message || JSON.stringify(e)
  for (const [re, key] of knownErrors) if (re.test(raw)) return t(key)
  return raw
}

const phaseKeys = new Map<string, MessageKey>([
  ['running', 'phase.running'], ['queued', 'phase.queued'], ['pending', 'phase.pending'],
  ['starting', 'phase.starting'], ['restarting', 'phase.restarting'], ['stopping', 'phase.stopping'],
  ['stopped', 'phase.stopped'], ['suspended', 'phase.suspended'], ['succeeded', 'phase.succeeded'],
  ['failed', 'phase.failed'], ['idle', 'phase.idle'], ['ready', 'phase.ready'],
  ['resizing', 'phase.resizing'],
  ['expired', 'phase.expired'],
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

// envErrors returns the 1-based numbers of the lines that are not NAME=value.
// Blank and comment lines are fine; linesToEnv would otherwise take a bare
// "KEY" as an empty value, or drop a "=value" line without a name.
export function envErrors(s: string): number[] {
  const bad: number[] = []
  s.split('\n').forEach((line, i) => {
    const text = line.trim()
    if (!text || text.startsWith('#')) return
    if (text.indexOf('=') <= 0) bad.push(i + 1)
  })
  return bad
}

// envMap turns the name/value list of a form into an EnvMap.
export function envMap(env: {name: string, value: string}[] = []): EnvMap {
  return Object.fromEntries(env.map(e => [e.name, e.value]))
}

// k8sNameAlphabet is the alphabet of Kubernetes' generated name suffixes: no
// vowels, so a random name never spells a word, and no look-alike characters.
const k8sNameAlphabet = 'bcdfghjklmnpqrstvwxz2456789'

// NAME_PATTERN and NAME_MAX are the rule for names a user types for a Kubernetes
// object that is a DNS label: lowercase letters, digits and dashes, starting
// with a letter and ending with a letter or digit. The backend (pkg/training,
// pkg/sandbox) rejects a leading digit, so the form must too. Sandboxes allow
// SANDBOX_NAME_MAX characters.
export const NAME_PATTERN = /^[a-z]([-a-z0-9]*[a-z0-9])?$/
export const NAME_MAX = 50
export const SANDBOX_NAME_MAX = 63

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

// tagCount is the number of tags across a repository's images. The registry's
// artifact count is digests, and an untagged digest has no tag to count.
export function tagCount(repo: {images?: {tags?: string[]}[]}): number {
  return (repo.images ?? []).reduce((n, img) => n + (img.tags?.filter(Boolean).length ?? 0), 0)
}

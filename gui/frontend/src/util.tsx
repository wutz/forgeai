import {app} from '../wailsjs/go/models'
import {MessageKey, getLocale, t} from './i18n'

// conflictErrors are the backend messages for a save made from a version that a
// later change has replaced: the Kubernetes conflict and the project check.
const conflictErrors = [/has been modified; please apply your changes/, /the project changed while you were editing/]

// knownErrors maps the backend messages users hit most to catalogue keys, so
// they read well in both languages. Other backend text is shown as written.
const knownErrors: [RegExp, MessageKey][] = [
  [/not connected: select a kubeconfig context/, 'error.notConnected'],
  [/is forbidden: /, 'error.forbidden'],
  ...conflictErrors.map((re): [RegExp, MessageKey] => [re, 'error.conflict']),
  [/volume "[^"]*" already exists \(check the trash\)/, 'volumes.errInTrash'],
  [/"[^"]*" already exists/, 'error.alreadyExists'],
  [/context deadline exceeded|Client\.Timeout exceeded/, 'error.timeout'],
  [/dial tcp|connection refused|no such host|i\/o timeout/, 'error.unreachable'],
  [/was not created by forgeai and cannot be (edited|deleted) here/, 'error.notCreatedByForgeai'],
  [/registry is not configured for this project/, 'error.registryNotConfigured'],
  [/was superseded by a newer connect/, 'error.connectionSuperseded'],
  [/has no pod: it is stopped or still starting/, 'error.sandboxNoPod'],
  [/start it before opening a terminal/, 'error.sandboxNotRunning'],
  [/worker \d+ has no pod/, 'error.workerNoPod'],
  [/instance type "[^"]*" not found/, 'error.instanceTypeNotFound'],
]

function rawMessage(e: unknown): string {
  return typeof e === 'string' ? e
    : e instanceof Error ? e.message
    : (e as { message?: string })?.message || JSON.stringify(e)
}

export function errText(e: unknown): string {
  const raw = rawMessage(e)
  for (const [re, key] of knownErrors) if (re.test(raw)) return t(key)
  // Backend text is English. In the Chinese UI it is framed as a failure, so the message is not a bare English line.
  return getLocale() === 'zh' ? t('error.unknown', {message: raw}) : raw
}

// isAlreadyInstalling reports whether an install was refused because the same
// component is installing now; that install's progress still arrives.
export function isAlreadyInstalling(e: unknown): boolean {
  return /is already being installed/.test(rawMessage(e))
}

// isConflict reports whether a save failed because the object changed since it was loaded.
export function isConflict(e: unknown): boolean {
  const raw = rawMessage(e)
  return conflictErrors.some(re => re.test(raw))
}

// latestAttempt is the attempt that decides a job family's state: the newest of
// the leader and its retries. The list keeps the leader's name, because that is
// what the list links to, but a leader whose retry ran is "Retried", and its
// live attempt is the one to count, filter, stop and rerun.
export function latestAttempt(j: app.Job): app.Job {
  return (j.retries ?? []).reduce((newest, r) => (r.created > newest.created ? r : newest), j)
}

// JobBucket is the group a job phase counts under: the list filters and the
// Overview counts use the same grouping, so the two always agree.
export type JobBucket = 'running' | 'queued' | 'stopped' | 'failed' | 'done'

export function jobBucket(phase: string): JobBucket {
  switch (phase) {
    // Stopping pods are still running, so the job counts as running until it stops.
    case 'Running':
    case 'Stopping':
      return 'running'
    case 'Queued':
    case 'Pending':
    case 'Restarting':
      return 'queued'
    case 'Stopped':
      return 'stopped'
    case 'Failed':
      return 'failed'
    default:
      return 'done'
  }
}

const phaseKeys = new Map<string, MessageKey>([
  ['running', 'phase.running'], ['queued', 'phase.queued'], ['pending', 'phase.pending'],
  ['starting', 'phase.starting'], ['restarting', 'phase.restarting'], ['stopping', 'phase.stopping'],
  ['stopped', 'phase.stopped'], ['suspended', 'phase.suspended'], ['succeeded', 'phase.succeeded'],
  ['failed', 'phase.failed'], ['retried', 'phase.retried'], ['idle', 'phase.idle'], ['ready', 'phase.ready'],
  ['resizing', 'phase.resizing'], ['deleting', 'phase.deleting'], ['lost', 'phase.lost'],
  ['expired', 'phase.expired'],
])

// phaseLabel is the text shown for a backend phase. Code compares phases by
// their English names, so only the displayed text is translated; an unknown
// phase is shown as it arrives.
export function phaseLabel(phase: string): string {
  const key = phaseKeys.get(phase.toLowerCase())
  return key ? t(key) : phase
}

// failureLabel is the text shown for a job's failure reason. The reasons are
// the stable codes the backend writes; an unknown code is shown as it arrives.
const failureReasons = new Set(['NodeFailure', 'Evicted', 'OOMKilled', 'ImagePullFailed', 'ConfigError', 'ExitCode', 'Unknown'])

export function failureLabel(reason: string): string {
  return failureReasons.has(reason) ? t(`jobs.reason.${reason}` as MessageKey) : reason
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

// envErrors returns the 1-based numbers of the lines that are not NAME=value,
// and of the lines that repeat a name already set above. Blank and comment lines
// are fine; linesToEnv would otherwise take a bare "KEY" as an empty value, drop
// a "=value" line without a name, or keep only the last of two same names.
// envNameRE is the name rule Kubernetes applies to container environment variables.
const envNameRE = /^[-._a-zA-Z][-._a-zA-Z0-9]*$/

export function envErrors(s: string): number[] {
  const bad: number[] = []
  const seen = new Set<string>()
  s.split('\n').forEach((line, i) => {
    const text = line.trim()
    if (!text || text.startsWith('#')) return
    const eq = text.indexOf('=')
    const name = eq <= 0 ? '' : text.slice(0, eq).trim()
    if (!envNameRE.test(name) || seen.has(name)) bad.push(i + 1)
    seen.add(name)
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
// SSH_NAME_MAX is the longest sandbox name with SSH on: its SSH Service is "<name>-ssh".
export const SSH_NAME_MAX = SANDBOX_NAME_MAX - '-ssh'.length

// MAX_RETRIES is the most retries a job may ask for; the backend enforces the same bound.
export const MAX_RETRIES = 20

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
  return isNaN(+d) ? iso : d.toLocaleString(getLocale())
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

// everyWhileVisible runs fn every ms while the window is visible, and once when
// it becomes visible again, so a hidden window makes no reads. It returns the
// function that stops both.
export function everyWhileVisible(fn: () => void, ms: number): () => void {
  const tick = () => { if (!document.hidden) fn() }
  const timer = setInterval(tick, ms)
  document.addEventListener('visibilitychange', tick)
  return () => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', tick)
  }
}

// formatGoDuration reads a Go duration ("1h30m0s") as "1 h 30 min", dropping the
// zero parts. A value it does not parse is shown as it arrives.
export function formatGoDuration(d: string): string {
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/.exec(d)
  if (!m || d === '') return d
  const parts = [
    +(m[1] ?? 0) > 0 && t('duration.hours', {count: +m[1]}),
    +(m[2] ?? 0) > 0 && t('duration.minutes', {count: +m[2]}),
    +(m[3] ?? 0) > 0 && t('duration.seconds', {count: +m[3]}),
  ].filter(Boolean)
  return parts.length ? parts.join(' ') : d
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

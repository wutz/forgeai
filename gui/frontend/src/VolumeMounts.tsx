import {volumes} from '../wailsjs/go/models'
import {Icon} from './ui'

// Mount is one volume mounted into a job's workers or a sandbox. Both forms
// carry this shape.
export type Mount = {volume: string, mountPath: string, subPath?: string, readOnly?: boolean}

// reservedPaths are mounted by forgeai itself: /dev/shm is the shared memory of
// every pod.
const reservedPaths = ['/dev/shm']

// phaseProblems says why a volume in another phase cannot be mounted.
const phaseProblems: Record<string, string> = {
  Pending: 'is still being provisioned',
  Lost: 'is lost',
  Deleting: 'is being deleted',
}

// mountable is a volume a pod can mount: bound, or bound and growing.
export const mountable = (v: volumes.Volume) => v.phase === 'Bound' || v.phase === 'Resizing'

// defaultPath is where a newly chosen volume is mounted until the user picks another path.
const defaultPath = (volume: string) => `/data/${volume}`

// autoPath matches the paths the field suggests itself, so choosing another
// volume moves a suggested path but keeps one the user typed.
const autoPath = /^\/data(\/[a-z0-9-]+)?$/

// mountProblems returns the problem with each mount, or '' when it is fine.
// The volumes are the project's live volumes; workers is the job's worker count.
export function mountProblems(mounts: Mount[], vols: volumes.Volume[], workers: number): string[] {
  return mounts.map((m, i) => {
    const volume = m.volume.trim()
    const path = m.mountPath.trim()
    const sub = (m.subPath || '').trim()
    const v = vols.find(x => x.name === volume)
    if (!volume) return 'Choose a volume'
    if (!v) return `${volume} is not a volume of this project`
    if (!mountable(v)) return `${volume} ${phaseProblems[v.phase] ?? `is ${v.phase.toLowerCase()}`} and cannot be mounted`
    if (!path) return 'Enter a mount path'
    if (!path.startsWith('/')) return 'The mount path must be absolute, starting with /'
    if (reservedPaths.includes(path)) return `${path} is reserved for shared memory`
    if (mounts.slice(0, i).some(o => o.mountPath.trim() === path)) return 'Another mount already uses this path'
    if (sub && (sub.startsWith('/') || sub.split('/').includes('..'))) return 'The sub path must be relative and stay inside the volume'
    if (workers > 1 && v.accessMode !== 'ReadWriteMany') {
      return `${v.accessMode} volumes attach to one node; a job with several workers needs a ReadWriteMany volume`
    }
    return ''
  })
}

// VolumeMountsField edits the volume mounts of a job or sandbox. Each mount
// picks one of the project's volumes, so a mount can only name a volume that
// exists and is ready; the volume's size and access mode are shown beside it.
// vols is null while the volumes load.
export function VolumeMountsField({mounts, onChange, vols, workers = 1}: {
  mounts: Mount[], onChange: (next: Mount[]) => void, vols: volumes.Volume[] | null, workers?: number,
}) {
  const problems = vols ? mountProblems(mounts, vols, workers) : []
  const choices = (vols ?? []).filter(mountable)
  const update = (i: number, patch: Partial<Mount>) =>
    onChange(mounts.map((m, j) => j === i ? {...m, ...patch} : m))

  // add mounts the first ready volume not mounted yet; a volume can be mounted twice.
  const add = () => {
    const next = choices.find(v => !mounts.some(m => m.volume === v.name)) ?? choices[0]
    if (!next) return
    onChange([...mounts, {volume: next.name, mountPath: defaultPath(next.name), subPath: '', readOnly: false}])
  }
  const pick = (i: number, name: string) => {
    const path = mounts[i].mountPath
    update(i, {volume: name, ...(!path || autoPath.test(path) ? {mountPath: defaultPath(name)} : {})})
  }

  return (
    <div className="mounts">
      {mounts.map((m, i) => {
        const v = vols?.find(x => x.name === m.volume)
        const problem = problems[i]
        return (
          <div className="mount-row" key={i}>
            <div className="mount-line">
              <select className="select mono" aria-label={`Volume of mount ${i + 1}`} value={m.volume}
                      onChange={e => pick(i, e.target.value)}>
                {!m.volume && <option value="">Choose a volume</option>}
                {vols && m.volume && !v && <option value={m.volume}>{m.volume} (not found)</option>}
                {(vols ?? []).map(x => (
                  <option key={x.name} value={x.name} disabled={!mountable(x)}>
                    {x.name}{mountable(x) ? '' : ` (${x.phase.toLowerCase()})`}
                  </option>
                ))}
              </select>
              <input className="input mono" aria-label={`Mount path of mount ${i + 1}`} value={m.mountPath}
                     autoCapitalize="none" autoCorrect="off" spellCheck={false}
                     placeholder="/data/<volume>" onChange={e => update(i, {mountPath: e.target.value})}/>
              <button type="button" className="icon-btn" aria-label={`Remove mount ${i + 1}`}
                      onClick={() => onChange(mounts.filter((_, j) => j !== i))}>
                <Icon name="close" size={14}/>
              </button>
            </div>
            <div className="mount-line">
              <input className="input mono" aria-label={`Sub path of mount ${i + 1}`} value={m.subPath || ''}
                     autoCapitalize="none" autoCorrect="off" spellCheck={false}
                     placeholder="sub path (optional)" onChange={e => update(i, {subPath: e.target.value})}/>
              <label className="checkbox">
                <input type="checkbox" checked={!!m.readOnly} onChange={e => update(i, {readOnly: e.target.checked})}/>
                Read only
              </label>
            </div>
            {v && <span className="mount-hint">
              {v.capacityGiB || v.requestedGiB} GiB · {v.accessMode} · {v.phase}
            </span>}
            {problem && <span className="mount-hint mount-hint-error" role="alert">{problem}</span>}
          </div>
        )
      })}
      <div className="mounts-foot">
        <button type="button" className="btn btn-sm" onClick={add} disabled={!vols || choices.length === 0}>
          <Icon name="plus" size={13}/>Add volume mount
        </button>
        {vols?.length === 0 && <span className="mount-hint">
          No volumes in this project yet. Create one under Storage › Volumes.
        </span>}
        {vols && vols.length > 0 && choices.length === 0 && <span className="mount-hint">
          None of the project's volumes is ready to mount.
        </span>}
        {!vols && <span className="mount-hint">Loading volumes…</span>}
      </div>
    </div>
  )
}

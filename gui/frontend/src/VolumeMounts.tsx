import {volumes} from '../wailsjs/go/models'
import {MessageKey, t} from './i18n'
import {Icon} from './ui'

// Mount is one volume mounted into a job's workers or a sandbox. Both forms
// carry this shape.
export type Mount = {volume: string, mountPath: string, subPath?: string, readOnly?: boolean}

// reservedPaths are mounted by forgeai itself: /dev/shm is the shared memory of
// every pod.
const reservedPaths = ['/dev/shm']

// phaseProblems names why a volume in another phase cannot be mounted.
const phaseProblems: Record<string, MessageKey> = {
  Lost: 'mounts.lost',
  Deleting: 'mounts.deleting',
}

// mountable is a volume a pod can mount: bound, bound and growing, or pending.
// A pending volume binds when its first pod uses it, so the pod waits for it.
export const mountable = (v: volumes.Volume) => v.phase === 'Bound' || v.phase === 'Resizing' || v.phase === 'Pending'

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
    if (!volume) return t('mounts.chooseVolume')
    if (!v) return t('mounts.notProject', {volume})
    if (!mountable(v)) {
      return t(phaseProblems[v.phase] ?? 'mounts.otherPhase', {volume, phase: v.phase.toLowerCase()})
    }
    if (!path) return t('mounts.noPath')
    if (!path.startsWith('/')) return t('mounts.absolute')
    if (reservedPaths.includes(path)) return t('mounts.reserved', {path})
    if (mounts.slice(0, i).some(o => o.mountPath.trim() === path)) return t('mounts.duplicate')
    if (sub && (sub.startsWith('/') || sub.split('/').includes('..'))) return t('mounts.subPathInvalid')
    if (workers > 1 && v.accessMode !== 'ReadWriteMany') return t('mounts.needRWX', {mode: v.accessMode})
    return ''
  })
}

// VolumeMountsField edits the volume mounts of a job or sandbox. Each mount
// picks one of the project's volumes, so a mount can only name a volume that
// exists and is ready; the volume's size and access mode are shown beside it.
// vols is null while the volumes load.
// loadError is set when the project's volumes could not be read: the field says
// so instead of waiting for them.
export function VolumeMountsField({mounts, onChange, vols, loadError, workers = 1}: {
  mounts: Mount[], onChange: (next: Mount[]) => void, vols: volumes.Volume[] | null, loadError?: string, workers?: number,
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
              <select className="select mono" aria-label={t('mounts.volumeOf', {index: i + 1})} value={m.volume}
                      onChange={e => pick(i, e.target.value)}>
                {!m.volume && <option value="">{t('mounts.chooseVolume')}</option>}
                {vols && m.volume && !v && <option value={m.volume}>{t('mounts.notFound', {volume: m.volume})}</option>}
                {(vols ?? []).map(x => (
                  <option key={x.name} value={x.name} disabled={!mountable(x)}>
                    {mountable(x) ? x.name : t('mounts.optionState', {name: x.name, phase: x.phase.toLowerCase()})}
                  </option>
                ))}
              </select>
              <input className="input mono" aria-label={t('mounts.pathOf', {index: i + 1})} value={m.mountPath}
                     autoCapitalize="none" autoCorrect="off" spellCheck={false}
                     placeholder={t('mounts.pathPlaceholder')} onChange={e => update(i, {mountPath: e.target.value})}/>
              <button type="button" className="icon-btn" aria-label={t('mounts.remove', {index: i + 1})}
                      onClick={() => onChange(mounts.filter((_, j) => j !== i))}>
                <Icon name="close" size={14}/>
              </button>
            </div>
            <div className="mount-line">
              <input className="input mono" aria-label={t('mounts.subPathOf', {index: i + 1})} value={m.subPath || ''}
                     autoCapitalize="none" autoCorrect="off" spellCheck={false}
                     placeholder={t('mounts.subPathPlaceholder')} onChange={e => update(i, {subPath: e.target.value})}/>
              <label className="checkbox">
                <input type="checkbox" checked={!!m.readOnly} onChange={e => update(i, {readOnly: e.target.checked})}/>
                {t('mounts.readOnly')}
              </label>
            </div>
            {v && <span className="mount-hint">
              {t('mounts.details', {size: v.capacityGiB || v.requestedGiB, mode: v.accessMode, phase: v.phase})}
            </span>}
            {problem && <span className="mount-hint mount-hint-error" role="alert">{problem}</span>}
          </div>
        )
      })}
      <div className="mounts-foot">
        <button type="button" className="btn btn-sm" onClick={add} disabled={!vols || choices.length === 0}>
          <Icon name="plus" size={13}/>{t('mounts.add')}
        </button>
        {vols?.length === 0 && <span className="mount-hint">{t('mounts.noVolumes')}</span>}
        {vols && vols.length > 0 && choices.length === 0 && <span className="mount-hint">{t('mounts.noneReady')}</span>}
        {!vols && <span className="mount-hint" role={loadError ? 'alert' : undefined}>
          {loadError ? t('mounts.loadFailed') : t('mounts.loading')}
        </span>}
      </div>
    </div>
  )
}

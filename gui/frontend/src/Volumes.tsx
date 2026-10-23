import {FormEvent, useEffect, useState} from 'react'
import {
  CreateVolume, ListVolumes, ResizeVolume, RestoreVolume, SetVolumeExpiry,
  TrashVolume, PurgeVolume, Config, FileServerPhase,
} from '../wailsjs/go/app/App'
import {app, volumes as vol} from '../wailsjs/go/models'
import {fmtDate, errText, generateName, phaseLabel} from './util'
import {t} from './i18n'
import {Badge, Card, CancelButton, Dialog, Empty, Icon, PageHeader, RefreshButton, useActive, useConfirm, Spinner} from './ui'

// Volumes manages shared filesystems: create, grow, trash (recycle bin),
// restore and purge. Deleting is delayed — the operator purges expired
// trashed volumes; users can restore or set the expiry.
export function Volumes() {
  // The lists are null until their first read, so a failed first read is not shown as an empty list.
  const [items, setItems] = useState<vol.Volume[] | null>(null)
  const [trash, setTrash] = useState<vol.Volume[] | null>(null)
  const [error, setError] = useState('')
  const [config, setConfig] = useState<app.PlatformConfig | null>(null)
  const [configError, setConfigError] = useState('')
  const [creating, setCreating] = useState(false)
  const [growing, setGrowing] = useState<vol.Volume | null>(null)
  const [recycling, setRecycling] = useState(false)
  const confirm = useConfirm()
  const active = useActive()

  // refresh rejects when a list fails, so the refresh button reports it; reload
  // is for the callers that have no place to show a rejection.
  const refresh = () => Promise.all([
    ListVolumes(false).then(vs => { setItems(vs); setError('') }),
    ListVolumes(true).then(vs => setTrash(vs)),
  ]).catch(e => {
    setError(errText(e))
    throw e
  })
  const reload = () => refresh().catch(() => undefined)
  useEffect(() => {
    if (!active) return
    reload()
    Config().then(setConfig).catch(e => setConfigError(errText(e)))
    const timer = setInterval(reload, 10000)
    return () => clearInterval(timer)
  }, [active])

  const totalGiB = (items ?? []).reduce((n, v) => n + (v.capacityGiB || v.requestedGiB || 0), 0)

  // The file server serves ReadWriteMany volumes, so trashing one rebuilds it
  // and ends any transfer in progress. The phase read does not wake or
  // heartbeat the server. When the phase cannot be read, the warning is shown
  // anyway: the server may be running.
  const trashVolume = (v: vol.Volume) => FileServerPhase()
    .catch(() => null)
    .then(phase => confirm.ask({
      title: t('volumes.trashTitle', {name: v.name}),
      message: <>
        {t('volumes.trashStays')}
        {(phase === null || phase === 'Ready' || phase === 'Starting') && v.accessMode === 'ReadWriteMany' && <>
          <br/><br/>
          <span className="banner banner-warn" style={{display: 'block'}}>
            {t('volumes.trashServerWarn')}
          </span>
        </>}
      </>,
      action: t('volumes.moveToTrash'), danger: true,
      run: () => TrashVolume(v.name).then(reload).catch(e => setError(errText(e))),
    }))

  return (
    <div className="page">
      <PageHeader eyebrow={t('storage.eyebrow')} title={t('volumes.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {t('volumes.summary', {count: items?.length ?? 0, size: totalGiB})}
                  </span>}
                  actions={<>
                    <button className="btn" onClick={() => setRecycling(true)}>
                      <Icon name="trash" size={14}/>{t('volumes.recycleBin')}
                      {(trash?.length ?? 0) > 0 && <Badge>{trash?.length}</Badge>}
                    </button>
                    <button className="btn" onClick={() => setCreating(true)}>
                      <Icon name="plus" size={14}/>{t('volumes.new')}
                    </button>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {configError && <div className="banner banner-error" role="alert">{configError}</div>}
      {creating && config && <CreateVolumeDialog config={config} onClose={() => setCreating(false)}
                                                 onCreated={refresh}/>}
      {growing && <ResizeVolumeDialog volume={growing} onClose={() => setGrowing(null)} onDone={refresh}/>}
      {recycling && <RecycleBinDialog volumes={trash ?? []} onChanged={refresh} onClose={() => setRecycling(false)}/>}
      {confirm.dialog}

      <Card>
        {!items ? (
          error ? null : <Spinner label={t('common.loading')}/>
        ) : items.length === 0 ? (
          <Empty icon="database" title={t('volumes.emptyTitle')}>{t('volumes.emptyHint')}</Empty>
        ) : (
          <table className="table">
            <thead>
            <tr>
              <th>{t('common.name')}</th><th>{t('volumes.type')}</th><th>{t('volumes.size')}</th><th>{t('volumes.phase')}</th>
              <th>{t('volumes.usedBy')}</th><th>{t('common.created')}</th><th/>
            </tr>
            </thead>
            <tbody>
            {items.map(v => (
              <tr key={v.name}>
                <td className="mono">
                  <span className="cell-main">{v.name}</span>
                  <span className="cell-sub">{v.storageClass}</span>
                </td>
                <td><Badge>{config?.storageTypes?.[v.type]?.description || v.type}</Badge></td>
                <td className="mono">{v.capacityGiB || v.requestedGiB} GiB
                  {v.phase === 'Resizing' && <span className="muted"> → {v.requestedGiB} GiB</span>}
                </td>
                <td><span className={`status status-${v.phase === 'Bound' ? 'running' : v.phase.toLowerCase()}`}>
                  <span className="dot" aria-hidden/>{v.phase === 'Bound' ? t('volumes.bound') : phaseLabel(v.phase)}</span></td>
                <td className="muted mono trunc" title={v.users?.join(', ')}>{v.users?.join(', ') || '—'}</td>
                <td className="muted">{fmtDate(v.created)}</td>
                <td>
                  <div className="actions">
                    <button className="btn btn-sm" disabled={v.phase !== 'Bound' && v.phase !== 'Resizing'}
                            title={v.phase === 'Pending' ? t('volumes.pendingHint') : undefined}
                            onClick={() => setGrowing(v)}>{t('volumes.grow')}</button>
                    <button className="btn btn-sm btn-danger" disabled={!!v.users?.length}
                            title={v.users?.length ? t('volumes.inUseHint') : undefined}
                            onClick={() => trashVolume(v)}>{t('common.delete')}</button>
                  </div>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  )
}

// RecycleBinDialog manages trashed volumes behind the header button, so the
// page carries no list of them. It sits over the page, so its own errors and
// purge confirmation render inside it rather than in the page banner.
function RecycleBinDialog({volumes, onChanged, onClose}: {
  volumes: vol.Volume[], onChanged: () => void, onClose: () => void
}) {
  const [error, setError] = useState('')
  const [expiring, setExpiring] = useState<vol.Volume | null>(null)
  const confirm = useConfirm()

  const restore = (v: vol.Volume) => RestoreVolume(v.name).then(onChanged).catch(e => setError(errText(e)))
  const purge = (v: vol.Volume) => confirm.ask({
    title: t('volumes.purgeTitle', {name: v.name}),
    message: t('volumes.purgeMessage'),
    action: t('volumes.purgeNow'), danger: true,
    run: () => PurgeVolume(v.name).then(onChanged).catch(e => setError(errText(e))),
  })

  return <>
    <Dialog title={t('volumes.recycleBin')} subtitle={t('volumes.trashEmptyHint')} width={720} onClose={onClose}>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {volumes.length === 0 ? (
        <Empty icon="trash" title={t('volumes.trashEmpty')}/>
      ) : (
        <table className="table recycle">
          <thead><tr><th>{t('common.name')}</th><th>{t('volumes.size')}</th><th>{t('volumes.purgeAt')}</th><th/></tr></thead>
          <tbody>
          {volumes.map(v => (
            <tr key={v.name} className="trashed">
              <td className="mono">{v.name}</td>
              <td className="mono">{v.capacityGiB || v.requestedGiB} GiB</td>
              <td className="muted">{v.expireAt ? fmtDate(v.expireAt) : '—'}</td>
              <td>
                <div className="actions">
                  <button className="btn btn-sm" onClick={() => restore(v)}>{t('volumes.restore')}</button>
                  <button className="btn btn-sm" onClick={() => setExpiring(v)}>{t('volumes.setExpiry')}</button>
                  <button className="btn btn-sm btn-danger" onClick={() => purge(v)}>{t('volumes.purgeNow')}</button>
                </div>
              </td>
            </tr>
          ))}
          </tbody>
        </table>
      )}
    </Dialog>
    {expiring && <ExpiryDialog volume={expiring} onClose={() => setExpiring(null)} onDone={onChanged}/>}
    {confirm.dialog}
  </>
}

function CreateVolumeDialog({config, onClose, onCreated}: {
  config: app.PlatformConfig, onClose: () => void, onCreated: () => void
}) {
  const [name, setName] = useState(() => generateName('volume'))
  const types = Object.entries(config.storageTypes || {})
  // The platform's default type is preselected; the first offered type is the
  // fallback for a config that names none.
  const [type, setType] = useState(() => config.defaultStorageType && config.storageTypes?.[config.defaultStorageType]
    ? config.defaultStorageType : types[0]?.[0] || '')
  const [size, setSize] = useState(100)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // Checked as the form reads them, so a bad value is named under its field
  // instead of a native browser bubble or a failed create.
  const nameOK = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(name) && name.length <= 50
  const sizeOK = Number.isInteger(size) && size >= 1
  const typeOK = type !== ''

  const save = (e: FormEvent) => {
    e.preventDefault()
    if (!nameOK || !sizeOK || !typeOK) return
    setBusy(true)
    CreateVolume(name, type, size).then(() => {
      onCreated()
      onClose()
    }).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  return (
    <Dialog title={t('volumes.new')} subtitle={t('volumes.createSubtitle')} width={440}
            onClose={onClose} busy={busy} footer={<>
              <CancelButton/>
              <button type="submit" form="create-volume" className="btn"
                      disabled={busy || !nameOK || !sizeOK || !typeOK}>
                {busy ? t('volumes.creating') : t('volumes.createVolume')}
              </button>
            </>}>
      <form id="create-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error" role="alert">{error}</div>}
        {!typeOK && <div className="banner banner-warn">{t('volumes.noTypes')}</div>}
        <div className="field">
          <label htmlFor="v-name">{t('common.name')}</label>
          <input id="v-name" className="input mono" value={name}
                 autoCapitalize="none" autoCorrect="off" spellCheck={false}
                 maxLength={50} aria-invalid={!nameOK}
                 onChange={e => setName(e.target.value)} required/>
          {!nameOK && name !== ''
            ? <span className="hint hint-error">{t('volumes.nameHint')}</span>
            : <span className="hint">{t('volumes.nameHelp')}</span>}
        </div>
        <div className="field">
          <label htmlFor="v-type">{t('volumes.type')}</label>
          <select id="v-type" className="select" value={type} disabled={!typeOK}
                  onChange={e => setType(e.target.value)}>
            {types.map(([kind, info]) => <option key={kind} value={kind}>
              {info.description || kind}
            </option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="v-size">{t('volumes.sizeGiB')}</label>
          <input id="v-size" className="input" type="number" min={1} step={1} value={size}
                 aria-invalid={!sizeOK} onChange={e => setSize(+e.target.value)} required/>
          {!sizeOK
            ? <span className="hint hint-error">{t('volumes.sizeInvalid')}</span>
            : <span className="hint">{t('volumes.sizeHelp')}</span>}
        </div>
      </form>
    </Dialog>
  )
}

// ResizeVolumeDialog grows a bound volume; Kubernetes cannot shrink it.
function ResizeVolumeDialog({volume, onClose, onDone}: {
  volume: vol.Volume, onClose: () => void, onDone: () => void
}) {
  const current = volume.capacityGiB || volume.requestedGiB
  const [size, setSize] = useState(current * 2)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const save = (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    ResizeVolume(volume.name, size).then(() => {
      onDone()
      onClose()
    }).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  return (
    <Dialog title={t('volumes.growTitle', {name: volume.name})}
            subtitle={t('volumes.growSubtitle', {size: current})}
            width={400} onClose={onClose} busy={busy} footer={<>
              <CancelButton/>
              <button type="submit" form="resize-volume" className="btn btn-primary"
                      disabled={busy || !(size > current)}>
                {busy ? t('volumes.growing') : t('volumes.growVolume')}
              </button>
            </>}>
      <form id="resize-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="field">
          <label htmlFor="v-grow">{t('volumes.newSize')}</label>
          <input id="v-grow" className="input" type="number" min={current + 1} step={1} value={size}
                 aria-invalid={!(size > current)}
                 onChange={e => setSize(+e.target.value)} required/>
          {!(size > current) && <span className="hint hint-error">
            {t('volumes.growTooSmall', {size: current})}
          </span>}
        </div>
      </form>
    </Dialog>
  )
}

// ExpiryDialog sets the day a trashed volume is purged. The day starts at
// local midnight, and the earliest choice is tomorrow.
function ExpiryDialog({volume, onClose, onDone}: {
  volume: vol.Volume, onClose: () => void, onDone: () => void
}) {
  const [day, setDay] = useState(volume.expireAt ? localDay(new Date(volume.expireAt)) : '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const tomorrow = localDay(new Date(Date.now() + 24 * 3600 * 1000))
  // Dates are YYYY-MM-DD, so a string compare orders them.
  const dayOK = !!day && day >= tomorrow

  const save = (e: FormEvent) => {
    e.preventDefault()
    if (!dayOK) return
    setBusy(true)
    SetVolumeExpiry(volume.name, new Date(`${day}T00:00:00`)).then(() => {
      onDone()
      onClose()
    }).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  return (
    <Dialog title={t('volumes.expiryTitle', {name: volume.name})} subtitle={t('volumes.expirySubtitle')}
            width={400} onClose={onClose} busy={busy} footer={<>
              <CancelButton/>
              <button type="submit" form="expiry-volume" className="btn btn-primary" disabled={busy || !dayOK}>
                {busy ? t('volumes.saving') : t('common.save')}
              </button>
            </>}>
      <form id="expiry-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="field">
          <label htmlFor="v-expiry">{t('volumes.purgeDay')}</label>
          <input id="v-expiry" className="input" type="date" min={tomorrow} value={day}
                 onChange={e => setDay(e.target.value)} aria-invalid={!dayOK} required/>
          {day !== '' && !dayOK && <span className="hint hint-error">{t('volumes.purgeDayTooSoon')}</span>}
        </div>
      </form>
    </Dialog>
  )
}

// localDay formats a date as yyyy-mm-dd in local time, the form of date inputs.
function localDay(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

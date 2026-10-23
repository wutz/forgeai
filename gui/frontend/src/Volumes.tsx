import {FormEvent, useEffect, useState} from 'react'
import {
  CreateVolume, ListVolumes, ResetVolumeExpiry, ResizeVolume, RestoreVolume, SetVolumeExpiry,
  TrashVolume, PurgeVolume, Config, FileServerPhase,
} from '../wailsjs/go/app/App'
import {app, volumes as vol} from '../wailsjs/go/models'
import {fmtDate, errText, everyWhileVisible, generateName, phaseLabel} from './util'
import {t} from './i18n'
import {Badge, Card, CancelButton, Dialog, Empty, HintButton, Icon, PageHeader, RefreshButton, useActive, useConfirm, Spinner} from './ui'

// Volumes manages shared filesystems: create, grow, trash (recycle bin),
// restore and purge. Deleting is delayed — the operator purges expired
// trashed volumes; users can restore or set the expiry.
export function Volumes() {
  // The lists are null until their first read, so a failed first read is not shown as an empty list.
  const [items, setItems] = useState<vol.Volume[] | null>(null)
  const [trash, setTrash] = useState<vol.Volume[] | null>(null)
  const [trashError, setTrashError] = useState('')
  const [error, setError] = useState('')
  const [config, setConfig] = useState<app.PlatformConfig | null>(null)
  const [configError, setConfigError] = useState('')
  const [creating, setCreating] = useState(false)
  const [growing, setGrowing] = useState<vol.Volume | null>(null)
  const [recycling, setRecycling] = useState(false)
  const confirm = useConfirm()
  const active = useActive()

  // refresh rejects when a list fails, so the refresh button reports it; reload
  // is for the callers that have no place to show a rejection. The recycle bin
  // reads on its own: its failure is shown in the bin, not as an empty bin.
  const refreshTrash = () => ListVolumes(true)
    .then(vs => { setTrash(vs); setTrashError('') }, e => setTrashError(errText(e)))
  const refresh = () => Promise.all([
    ListVolumes(false).then(vs => { setItems(vs); setError('') }),
    refreshTrash(),
  ]).catch(e => {
    setError(errText(e))
    throw e
  })
  const reload = () => refresh().catch(() => undefined)
  useEffect(() => {
    if (!active) return
    reload()
    return everyWhileVisible(reload, 10000)
  }, [active])

  // The platform config is read until it arrives, so a failed read is retried on
  // the same cadence instead of staying until the page is left and reopened.
  useEffect(() => {
    if (!active || config) return
    const load = () => Config().then(c => { setConfig(c); setConfigError('') }, e => setConfigError(errText(e)))
    load()
    return everyWhileVisible(load, 10000)
  }, [active, config])

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
            {t(phase === null ? 'volumes.trashServerUnknown' : 'volumes.trashServerWarn')}
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
                    {items !== null && t('volumes.summary', {count: items.length, size: totalGiB})}
                  </span>}
                  actions={<>
                    <button className="btn" onClick={() => setRecycling(true)}>
                      <Icon name="trash" size={14}/>{t('volumes.recycleBin')}
                      {(trash?.length ?? 0) > 0 && <Badge>{trash?.length}</Badge>}
                    </button>
                    <button className="btn" disabled={!config} onClick={() => setCreating(true)}>
                      <Icon name="plus" size={14}/>{t('volumes.new')}
                    </button>
                    <RefreshButton onRefresh={refresh}/>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {configError && <div className="banner banner-error" role="alert">{configError}</div>}
      {creating && config && <CreateVolumeDialog config={config} onClose={() => setCreating(false)}
                                                 onCreated={reload}/>}
      {growing && <ResizeVolumeDialog volume={growing} onClose={() => setGrowing(null)} onDone={reload}/>}
      {recycling && <RecycleBinDialog volumes={trash} loadError={trashError} onChanged={refresh} onClose={() => setRecycling(false)}/>}
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
                    <HintButton className="btn btn-sm" disabled={v.phase !== 'Bound' && v.phase !== 'Resizing'}
                                reason={v.phase === 'Pending' ? t('volumes.pendingHint') : undefined}
                                onClick={() => setGrowing(v)}>{t('volumes.grow')}</HintButton>
                    <HintButton className="btn btn-sm btn-danger" disabled={!!v.users?.length}
                                reason={v.users?.length ? t('volumes.inUseHint') : undefined}
                                onClick={() => trashVolume(v)}>{t('common.delete')}</HintButton>
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
function RecycleBinDialog({volumes, loadError, onChanged, onClose}: {
  volumes: vol.Volume[] | null, loadError: string, onChanged: () => void, onClose: () => void
}) {
  const [error, setError] = useState('')
  const [expiring, setExpiring] = useState<vol.Volume | null>(null)
  // busy is the volume whose restore or purge is in flight; its row's buttons
  // stay disabled until the call answers, so a double click sends one request.
  const [busy, setBusy] = useState('')
  const confirm = useConfirm()

  const act = (name: string, call: () => Promise<unknown>) => {
    setBusy(name)
    setError('')
    return call().then(onChanged).catch(e => setError(errText(e))).finally(() => setBusy(''))
  }
  const restore = (v: vol.Volume) => act(v.name, () => RestoreVolume(v.name))
  const purge = (v: vol.Volume) => confirm.ask({
    title: t('volumes.purgeTitle', {name: v.name}),
    message: t('volumes.purgeMessage'),
    action: t('volumes.purgeNow'), danger: true,
    run: () => act(v.name, () => PurgeVolume(v.name)),
  })

  return <>
    <Dialog title={t('volumes.recycleBin')} subtitle={t('volumes.trashEmptyHint')} onClose={onClose}>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {volumes === null ? (
        loadError
          ? <Empty icon="warning" title={t('volumes.trashLoadFailed')}>{loadError}</Empty>
          : <Spinner label={t('common.loading')}/>
      ) : volumes.length === 0 ? (
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
                  <button className="btn btn-sm" disabled={!!busy} onClick={() => restore(v)}>{t('volumes.restore')}</button>
                  <button className="btn btn-sm" disabled={!!busy} onClick={() => setExpiring(v)}>{t('volumes.setExpiry')}</button>
                  <button className="btn btn-sm btn-danger" disabled={!!busy} onClick={() => purge(v)}>{t('volumes.purgeNow')}</button>
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

// MAX_VOLUME_GIB is the largest volume the platform accepts (1 PiB); the backend rejects more.
const MAX_VOLUME_GIB = 1 << 20

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
  const sizeOK = Number.isInteger(size) && size >= 1 && size <= MAX_VOLUME_GIB
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
              <CancelButton disabled={busy}/>
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
          <input id="v-size" className="input" type="number" min={1} max={MAX_VOLUME_GIB} step={1} value={size}
                 aria-invalid={!sizeOK} onChange={e => setSize(+e.target.value)} required/>
          {!sizeOK
            ? <span className="hint hint-error">{t('volumes.sizeInvalid', {max: MAX_VOLUME_GIB})}</span>
            : <span className="hint">{t('volumes.sizeHelp', {max: MAX_VOLUME_GIB})}</span>}
        </div>
      </form>
    </Dialog>
  )
}

// ResizeVolumeDialog grows a bound volume; Kubernetes cannot shrink it.
function ResizeVolumeDialog({volume, onClose, onDone}: {
  volume: vol.Volume, onClose: () => void, onDone: () => void
}) {
  // The floor is the larger of the size in use and the size asked for: a resize that is
  // still settling has requested more than its capacity shows, and a smaller one is refused.
  const current = Math.max(volume.capacityGiB || 0, volume.requestedGiB || 0)
  // The default is the largest size the platform takes, so a very large volume doubled
  // past 1 PiB opens already invalid; the field is clamped and still must grow.
  const [size, setSize] = useState(Math.min(current * 2, MAX_VOLUME_GIB))
  // A resize only grows: the size is a whole number above the current one.
  const growOK = Number.isInteger(size) && size > current && size <= MAX_VOLUME_GIB
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
            width={440} onClose={onClose} busy={busy} footer={<>
              <CancelButton disabled={busy}/>
              <button type="submit" form="resize-volume" className="btn btn-primary"
                      disabled={busy || !growOK}>
                {busy ? t('volumes.growing') : t('volumes.growVolume')}
              </button>
            </>}>
      <form id="resize-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error" role="alert">{error}</div>}
        <div className="field">
          <label htmlFor="v-grow">{t('volumes.newSize')}</label>
          <input id="v-grow" className="input" type="number" min={current + 1} max={MAX_VOLUME_GIB} step={1} value={size}
                 aria-invalid={!growOK}
                 onChange={e => setSize(+e.target.value)} required/>
          {!growOK && <span className="hint hint-error">
            {size > MAX_VOLUME_GIB ? t('volumes.growTooLarge', {max: MAX_VOLUME_GIB}) : t('volumes.growTooSmall', {size: current})}
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
  // Tomorrow is the next calendar day, not 24 hours on: across a daylight-saving
  // change, 24 hours from late evening can land on the day after tomorrow.
  const next = new Date()
  next.setDate(next.getDate() + 1)
  const tomorrow = localDay(next)
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
  // The default is computed on the server from the project's retention, so the
  // dialog does not guess it; the recycle bin shows the date it lands on.
  const useDefault = () => {
    setBusy(true)
    setError('')
    ResetVolumeExpiry(volume.name).then(() => {
      onDone()
      onClose()
    }).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  return (
    <Dialog title={t('volumes.expiryTitle', {name: volume.name})} subtitle={t('volumes.expirySubtitle')}
            width={440} onClose={onClose} busy={busy} footer={<>
              <CancelButton disabled={busy}/>
              <button type="button" className="btn" disabled={busy} onClick={useDefault}>
                {t('volumes.useDefault')}
              </button>
              <button type="submit" form="expiry-volume" className="btn btn-primary" disabled={busy || !dayOK}>
                {busy ? t('volumes.saving') : t('common.save')}
              </button>
            </>}>
      <form id="expiry-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error" role="alert">{error}</div>}
        <div className="field">
          <label htmlFor="v-expiry">{t('volumes.purgeDay')}</label>
          <input id="v-expiry" className="input" type="date" min={tomorrow} value={day}
                 onChange={e => setDay(e.target.value)} aria-invalid={!dayOK} required/>
          {day !== '' && !dayOK
            ? <span className="hint hint-error">{t('volumes.purgeDayTooSoon')}</span>
            : <span className="hint">{t('volumes.useDefaultHint')}</span>}
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

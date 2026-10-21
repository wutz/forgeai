import {FormEvent, useEffect, useState} from 'react'
import {
  CreateVolume, ListVolumes, ResizeVolume, RestoreVolume, SetVolumeExpiry,
  TrashVolume, PurgeVolume, Config,
} from '../wailsjs/go/app/App'
import {app, volumes as vol} from '../wailsjs/go/models'
import {fmtDate, errText} from './util'
import {Badge, Card, Dialog, Empty, Icon, Meter, PageHeader, useConfirm} from './ui'

// Volumes manages shared filesystems: create, grow, trash (recycle bin),
// restore and purge. Deleting is delayed — the operator purges expired
// trashed volumes; users can restore or set the expiry.
export function Volumes() {
  const [items, setItems] = useState<vol.Volume[]>([])
  const [trash, setTrash] = useState<vol.Volume[]>([])
  const [error, setError] = useState('')
  const [config, setConfig] = useState<app.PlatformConfig | null>(null)
  const [configError, setConfigError] = useState('')
  const [creating, setCreating] = useState(false)
  const [growing, setGrowing] = useState<vol.Volume | null>(null)
  const [expiring, setExpiring] = useState<vol.Volume | null>(null)
  const confirm = useConfirm()

  const refresh = () => {
    ListVolumes(false).then(vs => { setItems(vs); setError('') }).catch(e => setError(errText(e)))
    ListVolumes(true).then(vs => setTrash(vs)).catch(e => setError(errText(e)))
  }
  useEffect(() => {
    refresh()
    Config().then(setConfig).catch(e => setConfigError(errText(e)))
    const t = setInterval(refresh, 10000)
    return () => clearInterval(t)
  }, [])

  const totalGiB = items.reduce((n, v) => n + (v.capacityGiB || v.requestedGiB || 0), 0)

  const trashVolume = (v: vol.Volume) => confirm.ask({
    title: `Move ${v.name} to the recycle bin?`,
    message: 'It stays restorable until it expires; the purge time follows the platform trash retention.',
    action: 'Move to recycle bin', danger: true,
    run: () => TrashVolume(v.name).then(refresh).catch(e => setError(errText(e))),
  })
  const purgeVolume = (v: vol.Volume) => confirm.ask({
    title: `Permanently delete ${v.name}?`,
    message: 'Its data is removed now. This cannot be undone.',
    action: 'Purge now', danger: true,
    run: () => PurgeVolume(v.name).then(refresh).catch(e => setError(errText(e))),
  })

  return (
    <div className="page">
      <PageHeader eyebrow="Storage" title="Volumes"
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {items.length} volumes · {totalGiB} GiB
                  </span>}
                  actions={<button className="btn btn-primary" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>New volume
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {configError && <div className="banner banner-error" role="alert">{configError}</div>}
      {creating && config && <CreateVolumeDialog config={config} onClose={() => setCreating(false)}
                                                 onCreated={refresh}/>}
      {growing && <ResizeVolumeDialog volume={growing} onClose={() => setGrowing(null)} onDone={refresh}/>}
      {expiring && <ExpiryDialog volume={expiring} onClose={() => setExpiring(null)} onDone={refresh}/>}
      {confirm.dialog}

      <Card>
        {items.length === 0 ? (
          <Empty icon="database" title="No volumes">Create one to share files between jobs;
            deleted volumes move to the recycle bin first.</Empty>
        ) : (
          <table className="table">
            <thead>
            <tr>
              <th>Name</th><th>Type</th><th>Size</th><th>Phase</th>
              <th>Used by</th><th>Created</th><th/>
            </tr>
            </thead>
            <tbody>
            {items.map(v => (
              <tr key={v.name}>
                <td className="mono">
                  <span className="cell-main">{v.name}</span>
                  <span className="cell-sub">{v.storageClass}</span>
                </td>
                <td><Badge>{v.type}</Badge></td>
                <td className="mono">{v.capacityGiB || v.requestedGiB} GiB
                  {v.phase === 'Resizing' && <span className="muted"> → {v.requestedGiB} GiB</span>}
                </td>
                <td><span className={`status status-${v.phase === 'Bound' ? 'running' : v.phase.toLowerCase()}`}>
                  <span className="dot" aria-hidden/>{v.phase}</span></td>
                <td className="muted mono trunc" title={v.users?.join(', ')}>{v.users?.join(', ') || '—'}</td>
                <td className="muted">{fmtDate(v.created)}</td>
                <td>
                  <div className="actions">
                    <button className="btn btn-sm" disabled={v.phase !== 'Bound' && v.phase !== 'Resizing'}
                            title={v.phase === 'Pending' ? 'Available once the volume is provisioned' : undefined}
                            onClick={() => setGrowing(v)}>Grow</button>
                    <button className="btn btn-sm btn-danger" disabled={!!v.users?.length}
                            title={v.users?.length ? 'In use by running pods' : undefined}
                            onClick={() => trashVolume(v)}>Delete</button>
                  </div>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </Card>

      <h2 className="subtitle">Recycle bin</h2>
      <Card>
        {trash.length === 0 ? (
          <Empty icon="trash" title="Recycle bin is empty">
            Deleted volumes stay here and can be restored until they expire.
          </Empty>
        ) : (
          <table className="table">
            <thead><tr><th>Name</th><th>Size</th><th>Purge at</th><th/></tr></thead>
            <tbody>
            {trash.map(v => (
              <tr key={v.name} className="trashed">
                <td className="mono">{v.name}</td>
                <td>{v.capacityGiB || v.requestedGiB} GiB</td>
                <td className="muted">{v.expireAt ? fmtDate(v.expireAt) : '—'}</td>
                <td>
                  <div className="actions">
                    <button className="btn btn-sm" onClick={() =>
                      RestoreVolume(v.name).then(refresh).catch(e => setError(errText(e)))}>Restore</button>
                    <button className="btn btn-sm" onClick={() => setExpiring(v)}>Set expiry</button>
                    <button className="btn btn-sm btn-danger" onClick={() => purgeVolume(v)}>Purge now</button>
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

function CreateVolumeDialog({config, onClose, onCreated}: {
  config: app.PlatformConfig, onClose: () => void, onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [type, setType] = useState(Object.keys(config.storageTypes || {})[0] || 'shared')
  const [size, setSize] = useState(100)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const save = (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    CreateVolume(name, type, size).then(() => {
      onCreated()
      onClose()
    }).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  const types = Object.entries(config.storageTypes || {})
  return (
    <Dialog title="New volume" subtitle="Backed by a project PersistentVolumeClaim" width={440}
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button type="submit" form="create-volume" className="btn btn-primary" disabled={busy}>
                {busy ? 'Creating…' : 'Create volume'}
              </button>
            </>}>
      <form id="create-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="field">
          <label htmlFor="v-name">Name</label>
          <input id="v-name" className="input mono" value={name} pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
                 maxLength={50} title="Lowercase letters, digits and dashes; up to 50 characters"
                 onChange={e => setName(e.target.value)} required/>
        </div>
        <div className="field">
          <label htmlFor="v-type">Type</label>
          <select id="v-type" className="select" value={type} onChange={e => setType(e.target.value)}>
            {types.map(([t, info]) => <option key={t} value={t}>
              {t}{info.description ? ` — ${info.description}` : ''}
            </option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="v-size">Size (GiB)</label>
          <input id="v-size" className="input" type="number" min={1} step={10} value={size}
                 onChange={e => setSize(+e.target.value)} required/>
        </div>
        <Meter value={size} max={Math.max(size, 100)} tone="muted"
               label={<span>Requested storage</span>} right={<span>{size} GiB</span>}/>
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
    <Dialog title={`Grow ${volume.name}`} subtitle={`Currently ${current} GiB; volumes only grow`}
            width={400} onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button type="submit" form="resize-volume" className="btn btn-primary" disabled={busy}>
                {busy ? 'Growing…' : 'Grow volume'}
              </button>
            </>}>
      <form id="resize-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="field">
          <label htmlFor="v-grow">New size (GiB)</label>
          <input id="v-grow" className="input" type="number" min={current + 1} step={10} value={size}
                 onChange={e => setSize(+e.target.value)} required/>
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

  const save = (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    SetVolumeExpiry(volume.name, new Date(`${day}T00:00:00`)).then(() => {
      onDone()
      onClose()
    }).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  return (
    <Dialog title={`Purge ${volume.name} on`} subtitle="Restore keeps the volume; purge is final"
            width={400} onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button type="submit" form="expiry-volume" className="btn btn-primary" disabled={busy || !day}>
                {busy ? 'Saving…' : 'Save'}
              </button>
            </>}>
      <form id="expiry-volume" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="field">
          <label htmlFor="v-expiry">Purge day</label>
          <input id="v-expiry" className="input" type="date" min={tomorrow} value={day}
                 onChange={e => setDay(e.target.value)} required/>
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

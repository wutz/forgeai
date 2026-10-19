import {useEffect, useState} from 'react'
import {
  CreateVolume, ListVolumes, ResizeVolume, RestoreVolume, SetVolumeExpiry,
  TrashVolume, PurgeVolume, Config,
} from '../wailsjs/go/app/App'
import {app, volumes as vol} from '../wailsjs/go/models'
import {fmtDate, errText} from './util'
import {Badge, Card, Dialog, Empty, Icon, Meter, PageHeader} from './ui'

// Volumes manages shared filesystems: create, grow, trash (recycle bin),
// restore and purge. Deleting is delayed — the operator purges expired
// trashed volumes; users can restore or set the expiry.
export function Volumes() {
  const [items, setItems] = useState<vol.Volume[]>([])
  const [trash, setTrash] = useState<vol.Volume[]>([])
  const [error, setError] = useState('')
  const [config, setConfig] = useState<app.PlatformConfig | null>(null)
  const [creating, setCreating] = useState(false)

  const refresh = () => {
    ListVolumes(false).then(vs => { setItems(vs); setError('') }).catch(e => setError(errText(e)))
    ListVolumes(true).then(vs => setTrash(vs)).catch(e => setError(errText(e)))
  }
  useEffect(() => {
    refresh()
    Config().then(setConfig).catch(() => {})
    const t = setInterval(refresh, 10000)
    return () => clearInterval(t)
  }, [])

  const totalGiB = items.reduce((n, v) => n + (v.capacityGiB || v.requestedGiB || 0), 0)

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
      {creating && config && <CreateVolumeDialog config={config} onClose={() => setCreating(false)}
                                                 onCreated={refresh}/>}

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
                            onClick={() => {
                              const size = prompt(`New size in GiB (current ${v.capacityGiB || v.requestedGiB}):`,
                                String(Math.max(v.capacityGiB, v.requestedGiB) * 2))
                              if (size) ResizeVolume(v.name, +size).then(refresh).catch(e => setError(errText(e)))
                            }}>Grow</button>
                    <button className="btn btn-sm btn-danger" onClick={() => {
                      const days = prompt('Delete to trash. Purge after days:', '7')
                      if (days) TrashVolume(v.name).then(refresh).catch(e => setError(errText(e)))
                    }}>Delete</button>
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
                    <button className="btn btn-sm" onClick={() => {
                      const date = prompt('Purge at (yyyy-mm-dd):', v.expireAt?.slice(0, 10) || '')
                      if (date) SetVolumeExpiry(v.name, new Date(date)).then(refresh)
                        .catch(e => setError(errText(e)))
                    }}>Set expiry</button>
                    <button className="btn btn-sm btn-danger" onClick={() =>
                      confirm(`Permanently delete ${v.name}? This cannot be undone.`) &&
                      PurgeVolume(v.name).then(refresh).catch(e => setError(errText(e)))}>Purge now</button>
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

  const save = () => {
    setBusy(true)
    CreateVolume(name, type, size).then(onCreated).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }
  const types = Object.entries(config.storageTypes || {})
  return (
    <Dialog title="New volume" subtitle="Backed by a project PersistentVolumeClaim" width={440}
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button className="btn btn-primary" onClick={save} disabled={busy || !name}>
                {busy ? 'Creating…' : 'Create volume'}
              </button>
            </>}>
      <form className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="field">
          <label htmlFor="v-name">Name</label>
          <input id="v-name" className="input mono" value={name}
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
                 onChange={e => setSize(+e.target.value)}/>
        </div>
        <Meter value={size} max={Math.max(size, 100)} tone="muted"
               label={<span>Requested storage</span>} right={<span>{size} GiB</span>}/>
      </form>
    </Dialog>
  )
}

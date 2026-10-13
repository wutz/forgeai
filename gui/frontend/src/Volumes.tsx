import {useEffect, useState} from 'react'
import {
  CreateVolume, ListVolumes, ResizeVolume, RestoreVolume, SetVolumeExpiry,
  TrashVolume, PurgeVolume, Config,
} from '../wailsjs/go/app/App'
import {app, volumes as vol} from '../wailsjs/go/models'
import {fmtDate, errText} from './util'

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

  return (
    <>
      {error && <div className="banner banner-error">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Volumes</h1>
        <span className="muted">{items.length}</span>
        <span className="spacer"/>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>New volume</button>
      </div>
      {creating && config && <CreateVolumeDialog config={config} onClose={() => setCreating(false)}
                                                 onCreated={refresh}/>}

      <div className="card">
        {items.length === 0 && <div className="empty">No volumes.</div>}
        {items.length > 0 && (
          <table className="table">
            <thead>
            <tr>
              <th>Name</th><th>Type</th><th>Size</th><th>Phase</th>
              <th>Used by</th><th>Created</th>
            </tr>
            </thead>
            <tbody>
            {items.map(v => (
              <tr key={v.name}>
                <td className="mono">{v.name}</td>
                <td>{v.type}</td>
                <td>{v.capacityGiB || v.requestedGiB} GiB
                  {v.phase === 'Resizing' && <span className="muted"> → {v.requestedGiB} GiB</span>}
                </td>
                <td>{v.phase}</td>
                <td className="muted mono">{v.users?.join(', ') || '—'}</td>
                <td className="muted">{fmtDate(v.created)}</td>
                <td className="actions">
                  <button className="btn btn-sm" onClick={() => {
                    const size = prompt(`New size in GiB (current ${v.capacityGiB || v.requestedGiB}):`,
                      String(Math.max(v.capacityGiB, v.requestedGiB) * 2))
                    if (size) ResizeVolume(v.name, +size).then(refresh).catch(e => setError(errText(e)))
                  }}>Grow</button>
                  <button className="btn btn-sm btn-danger" onClick={() => {
                    const days = prompt('Delete to trash. Purge after days:', '7')
                    if (days) TrashVolume(v.name).then(refresh).catch(e => setError(errText(e)))
                  }}>Delete</button>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </div>

      <h2 className="subtitle">Recycle bin</h2>
      <div className="card">
        {trash.length === 0 && <div className="empty">Empty. Deleted volumes stay here and can be
          restored until they expire ({'7d'}).</div>}
        {trash.length > 0 && (
          <table className="table">
            <thead><tr><th>Name</th><th>Size</th><th>Purge at</th><th/></tr></thead>
            <tbody>
            {trash.map(v => (
              <tr key={v.name} className="trashed">
                <td className="mono">{v.name}</td>
                <td>{v.capacityGiB || v.requestedGiB} GiB</td>
                <td className="muted">{v.expireAt ? fmtDate(v.expireAt) : '—'}</td>
                <td className="actions">
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
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </div>
    </>
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
    <div className="overlay" onClick={e => e.target === e.currentTarget && onClose()}>
      <div className="dialog" style={{width: 440}}>
        <form onSubmit={e => { e.preventDefault(); save() }}>
          <h2>New volume</h2>
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
        </form>
        <div className="dialog-foot">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" onClick={save} disabled={busy || !name}>
            {busy ? 'Creating…' : 'Create volume'}
          </button>
        </div>
      </div>
    </div>
  )
}

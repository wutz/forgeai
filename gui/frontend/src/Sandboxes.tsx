import {useEffect, useState} from 'react'
import {
  DeleteSandbox, GetSandbox, KeepSandboxAwake, ListSandboxes, SandboxLogs, StartSandbox, StopSandbox,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {SandboxCreateDialog} from './SandboxCreateDialog'
import {Status, envToLines, fmtDate, errText} from './util'

// HEARTBEAT keeps a sandbox the user is looking at out of the idle policy:
// the operator suspends a sandbox only after sandbox.idleTimeout without one.
const HEARTBEAT = 60_000

// Sandboxes is the view of the project's agent sandboxes (agents.x-k8s.io).
export function Sandboxes() {
  const [boxes, setBoxes] = useState<app.Sandbox[]>([])
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const refresh = () => {
    ListSandboxes().then(bs => {
      setBoxes(bs)
      setError('')
    }).catch(e => setError(errText(e)))
  }
  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 5000)
    return () => clearInterval(t)
  }, [])

  return (
    <>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Sandboxes</h1>
        <span className="muted">{boxes.length}</span>
        <span className="spacer"/>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>New sandbox</button>
      </div>
      {creating && <SandboxCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}
      <div className="split">
        <SandboxTable boxes={boxes} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {selected && <SandboxDetail name={selected} onDone={refresh}/>}
      </div>
    </>
  )
}

function SandboxTable({boxes, selected, onSelect}: {
  boxes: app.Sandbox[], selected: string | null, onSelect: (name: string) => void
}) {
  if (!boxes.length) {
    return <div className="card"><div className="empty">
      No sandboxes yet. Create one to run an agent in an isolated, resumable environment.
    </div></div>
  }
  return (
    <div className="card">
      <table className="table">
        <thead>
        <tr>
          <th>Name</th>
          <th>Status</th>
          <th>Isolation</th>
          <th>Resources</th>
          <th>Endpoint</th>
          <th>Age</th>
        </tr>
        </thead>
        <tbody>
        {boxes.map(b => (
          <tr key={b.name} className={selected === b.name ? 'selected' : ''}
              onClick={() => onSelect(b.name)}>
            <td className="mono">{b.name}</td>
            <td><Status phase={b.phase}/>{b.message && b.phase !== 'Running' &&
              <span className="muted" title={b.message}> {short(b.message)}</span>}</td>
            <td className="mono">{b.runtimeClass || 'runc'}</td>
            <td className="mono">{b.cpu || '—'}/{b.memory || '—'}{b.gpu ? ` · ${b.gpu} GPU` : ''}</td>
            <td className="mono trunc">{b.serviceFQDN || b.podIPs?.[0] || '—'}</td>
            <td>{b.age}</td>
          </tr>
        ))}
        </tbody>
      </table>
    </div>
  )
}

function SandboxDetail({name, onDone}: { name: string, onDone: () => void }) {
  const [box, setBox] = useState<app.Sandbox | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<'spec' | 'log'>('spec')
  const [log, setLog] = useState('')
  const act = (p: Promise<unknown>) => p.then(onDone).catch(e => setError(errText(e)))

  useEffect(() => {
    const load = () => GetSandbox(name).then(setBox).catch(e => setError(errText(e)))
    load()
    const t = setInterval(load, 5000)
    // Keep the sandbox awake while its detail is open and running.
    const beat = setInterval(() => KeepSandboxAwake(name).catch(() => {}), HEARTBEAT)
    KeepSandboxAwake(name).catch(() => {})
    return () => {
      clearInterval(t)
      clearInterval(beat)
    }
  }, [name])

  if (!box) return <div className="card"><div className="empty">{error || '…'}</div></div>
  const running = box.phase === 'Running' || box.phase === 'Pending'

  return (
    <div className="card detail">
      <div className="card-head">
        <h3 className="mono">{box.name}</h3>
        <Status phase={box.phase}/>
        <span className="spacer"/>
        <button className="btn btn-sm" onClick={() =>
          SandboxLogs(box.name, 500).then(v => { setLog(v); setTab('log') }).catch(e => setError(errText(e)))}>
          Logs
        </button>
        {running && <button className="btn btn-sm" onClick={() => act(StopSandbox(box.name))}>Stop</button>}
        {!running && <button className="btn btn-sm btn-primary" onClick={() => act(StartSandbox(box.name))}>
          Start
        </button>}
      </div>
      {error && <div className="banner banner-error">{error}</div>}

      <div className="section">
        <dl className="kv">
          <dt>Image</dt><dd className="mono">{box.image}</dd>
          <dt>Isolation</dt><dd className="mono">{box.runtimeClass || 'runc (node runtime)'}</dd>
          <dt>Instance</dt><dd className="mono">{box.instanceType || '—'}</dd>
          <dt>CPU / memory</dt><dd className="mono">{box.cpu || '—'} / {box.memory || '—'}
            {box.gpu ? ` · ${box.gpu} GPU` : ''}</dd>
          <dt>Volumes</dt><dd className="mono">{box.volumes?.join(', ') || '—'}</dd>
          {box.serviceFQDN && <><dt>Service</dt><dd className="mono">{box.serviceFQDN}</dd></>}
          {box.podIPs && box.podIPs.length > 0 && <><dt>Pod IPs</dt><dd className="mono">{box.podIPs.join(', ')}</dd></>}
          {box.nodeName && <><dt>Node</dt><dd className="mono">{box.nodeName}</dd></>}
          <dt>Pod</dt><dd className="mono">{box.podName || '—'}
            {box.podPhase ? ` · ${box.podPhase}` : ''}</dd>
          {box.stopped && <><dt>Stopped</dt><dd>suspended; start it to resume where it left off</dd></>}
          {box.idleSince && <><dt>Idle since</dt><dd>{fmtDate(box.idleSince)}</dd></>}
          {box.shutdownTime && <><dt>Auto-stop</dt><dd>{fmtDate(box.shutdownTime)}</dd></>}
          {box.message && <><dt>Status</dt><dd>{box.message}</dd></>}
          <dt>Created</dt><dd>{fmtDate(box.created)}</dd>
        </dl>
        <div className="actions" style={{marginTop: 12}}>
          <button className="btn btn-sm btn-danger"
                  onClick={() => confirm(`Delete sandbox ${box.name}? Its pod and volumes are deleted.`) &&
                    act(DeleteSandbox(box.name))}>Delete</button>
        </div>
      </div>

      <div className="section">
        <div className="tabs">
          <button className={`btn btn-sm ${tab === 'spec' ? 'btn-primary' : ''}`}
                  onClick={() => setTab('spec')}>Env</button>
          {log && <button className={`btn btn-sm ${tab === 'log' ? 'btn-primary' : ''}`}
                  onClick={() => setTab('log')}>Log</button>}
        </div>
        {tab === 'log' && <pre className="logs">{log || 'No output'}</pre>}
        {tab === 'spec' && <pre className="code">{box.command ? `$ ${box.command}\n\n` : ''}
          {envToLines(box.env) || '—'}</pre>}
      </div>
    </div>
  )
}

function short(msg: string): string {
  return msg.length > 40 ? msg.slice(0, 40) + '…' : msg
}

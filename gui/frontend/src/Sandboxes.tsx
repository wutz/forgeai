import {useEffect, useState} from 'react'
import {
  DeleteSandbox, GetSandbox, KeepSandboxAwake, ListSandboxes, SandboxLogs, StartSandbox, StopSandbox,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {SandboxCreateDialog} from './SandboxCreateDialog'
import {Status, envToLines, fmtDate, errText} from './util'
import {Badge, Card, Empty, Icon, PageHeader, Spinner, Tabs} from './ui'

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

  const running = boxes.filter(b => b.phase === 'Running').length

  return (
    <div className="page">
      <PageHeader eyebrow="Workspace" title="Sandboxes"
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {running} running of {boxes.length}
                  </span>}
                  actions={<button className="btn btn-primary" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>New sandbox
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
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
    </div>
  )
}

function SandboxTable({boxes, selected, onSelect}: {
  boxes: app.Sandbox[], selected: string | null, onSelect: (name: string) => void
}) {
  if (!boxes.length) {
    return <Card><Empty icon="box" title="No sandboxes yet">
      Create one to run an agent in an isolated, resumable environment.</Empty></Card>
  }
  return (
    <Card>
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
            <td className="mono">
              <span className="cell-main">{b.name}</span>
              <span className="cell-sub">{b.image.split('/').pop()}</span>
            </td>
            <td><Status phase={b.phase}/>{b.message && b.phase !== 'Running' &&
              <div className="cell-sub" title={b.message}>{short(b.message)}</div>}</td>
            <td>{b.runtimeClass ? <Badge>{b.runtimeClass}</Badge> : <span className="muted">runc</span>}</td>
            <td className="mono">{b.cpu || '—'}/{b.memory || '—'}{b.gpu ? ` · ${b.gpu} GPU` : ''}</td>
            <td className="mono trunc" title={b.serviceFQDN || b.podIPs?.[0]}>{b.serviceFQDN || b.podIPs?.[0] || '—'}</td>
            <td className="muted">{b.age}</td>
          </tr>
        ))}
        </tbody>
      </table>
    </Card>
  )
}

type Tab = 'overview' | 'log' | 'env'

function SandboxDetail({name, onDone}: { name: string, onDone: () => void }) {
  const [box, setBox] = useState<app.Sandbox | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('overview')
  const [log, setLog] = useState('')
  const act = (p: Promise<unknown>) => p.then(onDone).catch(e => setError(errText(e)))

  useEffect(() => {
    setBox(null)
    setLog('')
    setTab('overview')
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

  if (!box) return <Card><Spinner label={error || 'Loading sandbox…'}/></Card>
  const running = box.phase === 'Running' || box.phase === 'Pending'

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h3 className="mono">{box.name}</h3>
          <div className="card-sub">{box.runtimeClass || 'runc'} · created {fmtDate(box.created)}</div>
        </div>
        <div className="card-head-actions"><Status phase={box.phase}/></div>
      </div>
      {error && <div className="banner banner-error">{error}</div>}

      <div className="detail-actions">
        <button className="btn btn-sm" onClick={() =>
          SandboxLogs(box.name, 500).then(v => { setLog(v); setTab('log') }).catch(e => setError(errText(e)))}>
          <Icon name="scroll" size={13}/>Logs
        </button>
        {running && <button className="btn btn-sm" onClick={() => act(StopSandbox(box.name))}>
          <Icon name="square" size={12}/>Stop
        </button>}
        {!running && <button className="btn btn-sm btn-accent" onClick={() => act(StartSandbox(box.name))}>
          <Icon name="play" size={12}/>Start
        </button>}
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" onClick={() =>
          confirm(`Delete sandbox ${box.name}? Its pod and volumes are deleted.`) &&
          act(DeleteSandbox(box.name))}><Icon name="trash" size={13}/>Delete</button>
      </div>

      <div className="section">
        <Tabs tabs={[
          {value: 'overview', label: 'Overview', icon: 'grid'},
          {value: 'log', label: 'Log', icon: 'scroll'},
          {value: 'env', label: 'Env', icon: 'layers'},
        ]} value={tab} onChange={setTab}/>

        {tab === 'overview' && (
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
            <dt>Pod</dt><dd className="mono">{box.podName || '—'}{box.podPhase ? ` · ${box.podPhase}` : ''}</dd>
            {box.stopped && <><dt>Stopped</dt><dd>suspended; start it to resume where it left off</dd></>}
            {box.idleSince && <><dt>Idle since</dt><dd>{fmtDate(box.idleSince)}</dd></>}
            {box.shutdownTime && <><dt>Auto-stop</dt><dd>{fmtDate(box.shutdownTime)}</dd></>}
            {box.message && <><dt>Status</dt><dd>{box.message}</dd></>}
            <dt>Created</dt><dd>{fmtDate(box.created)}</dd>
          </dl>
        )}
        {tab === 'log' && <pre className="logs">{log || 'No output'}</pre>}
        {tab === 'env' && <pre className="code">{box.command ? `$ ${box.command}\n\n` : ''}
          {envToLines(box.env) || '—'}</pre>}
      </div>
    </Card>
  )
}

function short(msg: string): string {
  return msg.length > 48 ? msg.slice(0, 48) + '…' : msg
}

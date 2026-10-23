import {useEffect, useState} from 'react'
import {
  DeleteSandbox, GetSandbox, KeepSandboxAwake, ListSandboxes, SandboxLogs, StartSandbox, StopSandbox,
} from '../wailsjs/go/app/App'
import {app, sandbox} from '../wailsjs/go/models'
import {SandboxCreateDialog} from './SandboxCreateDialog'
import {SandboxTemplateDialog} from './SandboxTemplates'
import {SandboxTerminal} from './SandboxTerminal'
import {Status, envToLines, fmtDate, errText} from './util'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Spinner, Tabs, useActive, useConfirm} from './ui'
import {t} from './i18n'

// HEARTBEAT is how often an open sandbox detail resets the sandbox's idle
// clock. The operator suspends a sandbox once it has been idle for its idle
// timeout, so the beats stop when the detail closes.
const HEARTBEAT = 60_000

// Sandboxes is the view of the project's agent sandboxes (agents.x-k8s.io).
export function Sandboxes() {
  const [boxes, setBoxes] = useState<app.Sandbox[]>([])
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const active = useActive()

  const refresh = () => {
    ListSandboxes().then(bs => {
      setBoxes(bs)
      setError('')
    }).catch(e => setError(errText(e)))
  }
  useEffect(() => {
    if (!active) return
    refresh()
    const timer = setInterval(refresh, 5000)
    return () => clearInterval(timer)
  }, [active])

  // Same definition as the detail view: a Pending sandbox is on its way up.
  const running = boxes.filter(b => b.phase === 'Running' || b.phase === 'Pending').length

  return (
    <div className="page">
      <PageHeader eyebrow={t('sandboxes.eyebrow')} title={t('sandboxes.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {t('sandboxes.runningCount', {running, total: boxes.length})}
                  </span>}
                  actions={<button className="btn btn-primary" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>{t('sandboxes.new')}
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
        {selected && <SandboxDetail name={selected} onDone={refresh}
                                    onDeleted={() => { setSelected(null); refresh() }}/>}
      </div>
    </div>
  )
}

function SandboxTable({boxes, selected, onSelect}: {
  boxes: app.Sandbox[], selected: string | null, onSelect: (name: string) => void
}) {
  if (!boxes.length) {
    return <Card><Empty icon="box" title={t('sandboxes.emptyTitle')}>
      {t('sandboxes.emptyHint')}</Empty></Card>
  }
  return (
    <Card>
      <table className="table">
        <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('common.status')}</th>
          <th>{t('sandboxes.isolation')}</th>
          <th>{t('sandboxes.resources')}</th>
          <th>{t('sandboxes.endpoint')}</th>
          <th>{t('sandboxes.age')}</th>
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

type Tab = 'overview' | 'terminal' | 'log' | 'env'

// sandboxTemplateForm is the sandbox template a sandbox's agent is saved as.
// Sizing, placement, volumes and lifetime are per sandbox, so they are left out.
function sandboxTemplateForm(box: app.Sandbox) {
  return sandbox.TemplateForm.createFrom({
    name: '', description: '', image: box.image, command: box.command || '',
    env: Object.entries(box.env || {}).map(([name, value]) => ({name, value})),
    runtimeClass: box.runtimeClass || '',
  })
}

function SandboxDetail({name, onDone, onDeleted}: {
  name: string, onDone: () => void, onDeleted: () => void
}) {
  const [box, setBox] = useState<app.Sandbox | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('overview')
  const [log, setLog] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedTemplate, setSavedTemplate] = useState('')
  // pending is the transition in flight. It stays set until the phase reaches
  // its target, because suspending or resuming takes the controller a while.
  const [pending, setPending] = useState<'start' | 'stop' | null>(null)
  const active = useActive()
  const act = (p: Promise<unknown>) => p.then(onDone).catch(e => setError(errText(e)))
  const confirm = useConfirm()
  const transition = (kind: 'start' | 'stop', p: Promise<unknown>) => {
    setPending(kind)
    p.then(onDone).catch(e => {
      setError(errText(e))
      setPending(null)
    })
  }
  const phase = box?.phase

  useEffect(() => {
    if (!phase || !pending) return
    const up = phase === 'Running' || phase === 'Pending'
    if (pending === 'start' ? up : !up) setPending(null)
  }, [phase, pending])

  useEffect(() => {
    setBox(null)
    setLog('')
    setTab('overview')
    setPending(null)
    setSaving(false)
    setSavedTemplate('')
  }, [name])

  // The poll and heartbeat run only while the view is on screen: a hidden
  // detail lets the sandbox go idle, as it does when the detail closes.
  useEffect(() => {
    if (!active) return
    const load = () => GetSandbox(name).then(setBox).catch(e => setError(errText(e)))
    load()
    const timer = setInterval(load, 5000)
    // Keep the sandbox awake while its detail is open and running.
    const beat = setInterval(() => KeepSandboxAwake(name).catch(() => {}), HEARTBEAT)
    KeepSandboxAwake(name).catch(() => {})
    return () => {
      clearInterval(timer)
      clearInterval(beat)
    }
  }, [name, active])

  if (!box) return <Card><Spinner label={error || t('sandboxes.loading')}/></Card>
  const running = box.phase === 'Running' || box.phase === 'Pending'
  // showLog rejects when the logs cannot be read; the banner shows why.
  const showLog = () =>
    SandboxLogs(box.name, 500).then(v => { setLog(v); setTab('log') }, e => {
      setError(errText(e))
      throw e
    })
  // Selecting the log tab loads the logs, since the tab is the only entry point.
  const selectTab = (next: Tab) => {
    if (next === 'log') showLog().catch(() => {})
    else setTab(next)
  }

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h3 className="mono">{box.name}</h3>
          <div className="card-sub">{box.runtimeClass || 'runc'} · {t('sandboxes.createdAt', {date: fmtDate(box.created)})}</div>
        </div>
        <div className="card-head-actions"><Status phase={box.phase}/></div>
      </div>
      {error && <div className="banner banner-error">{error}</div>}
      {savedTemplate && <div className="banner" role="status">{t('sandboxes.savedTemplate', {name: savedTemplate})}</div>}
      {confirm.dialog}
      {saving && <SandboxTemplateDialog initial={sandboxTemplateForm(box)} onClose={() => setSaving(false)}
                                        onSaved={n => { setSaving(false); setSavedTemplate(n) }}/>}

      <div className="detail-actions">
        {running && <button className="btn btn-sm" disabled={pending !== null}
                           onClick={() => transition('stop', StopSandbox(box.name))}>
          {pending === 'stop' ? <span className="spinner" aria-hidden/> : <Icon name="square" size={12}/>}
          {pending === 'stop' ? t('sandboxes.stopping') : t('common.stop')}
        </button>}
        {!running && <button className="btn btn-sm btn-accent" disabled={pending !== null}
                             onClick={() => transition('start', StartSandbox(box.name))}>
          {pending === 'start' ? <span className="spinner" aria-hidden/> : <Icon name="play" size={12}/>}
          {pending === 'start' ? t('sandboxes.starting') : t('common.start')}
        </button>}
        <button className="btn btn-sm" onClick={() => setSaving(true)}>
          <Icon name="layers" size={13}/>{t('sandboxes.saveAsTemplate')}
        </button>
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" onClick={() => confirm.ask({
          title: t('sandboxes.deleteTitle', {name: box.name}),
          message: t('sandboxes.deleteMessage'),
          action: t('sandboxes.deleteAction'), danger: true,
          run: () => DeleteSandbox(box.name).then(onDeleted).catch(e => setError(errText(e))),
        })}><Icon name="trash" size={13}/>{t('common.delete')}</button>
      </div>

      <div className="section">
        <Tabs tabs={[
          {value: 'overview', label: t('sandboxes.tabOverview'), icon: 'grid'},
          {value: 'terminal', label: t('sandboxes.tabTerminal'), icon: 'terminal'},
          {value: 'log', label: t('sandboxes.tabLog'), icon: 'scroll'},
          {value: 'env', label: t('sandboxes.tabEnv'), icon: 'layers'},
        ]} value={tab} onChange={selectTab}/>

        {tab === 'overview' && (
          <dl className="kv">
            <dt>{t('common.image')}</dt><dd className="mono">{box.image}</dd>
            <dt>{t('sandboxes.isolation')}</dt><dd className="mono">{box.runtimeClass || t('sandboxes.runcNode')}</dd>
            <dt>{t('sandboxes.instance')}</dt><dd className="mono">{box.instanceType || '—'}</dd>
            <dt>{t('sandboxes.cpuMemory')}</dt><dd className="mono">{box.cpu || '—'} / {box.memory || '—'}
              {box.gpu ? ` · ${box.gpu} GPU` : ''}</dd>
            <dt>{t('sandboxes.volumes')}</dt><dd className="mono">{box.volumes?.join(', ') || '—'}</dd>
            {box.serviceFQDN && <><dt>{t('sandboxes.service')}</dt><dd className="mono">{box.serviceFQDN}</dd></>}
            {box.podIPs && box.podIPs.length > 0 && <><dt>{t('sandboxes.podIps')}</dt><dd className="mono">{box.podIPs.join(', ')}</dd></>}
            {box.nodeName && <><dt>{t('sandboxes.node')}</dt><dd className="mono">{box.nodeName}</dd></>}
            <dt>{t('sandboxes.pod')}</dt><dd className="mono">{box.podName || '—'}{box.podPhase ? ` · ${box.podPhase}` : ''}</dd>
            {box.stopped && <><dt>{t('sandboxes.stoppedLabel')}</dt><dd>{t('sandboxes.suspendedNote')}</dd></>}
            {box.idleSince && <><dt>{t('sandboxes.idleSince')}</dt><dd>{fmtDate(box.idleSince)}</dd></>}
            {box.shutdownTime && <><dt>{t('sandboxes.autoStop')}</dt><dd>{fmtDate(box.shutdownTime)}</dd></>}
            {box.message && <><dt>{t('common.status')}</dt><dd>{box.message}</dd></>}
            <dt>{t('common.created')}</dt><dd>{fmtDate(box.created)}</dd>
          </dl>
        )}
        {tab === 'terminal' && (box.podPhase === 'Running'
          ? <SandboxTerminal name={box.name}/>
          : <p className="muted">{t('sandboxes.startToOpenTerminal')}</p>)}
        {tab === 'log' && (
          <>
            <div className="log-bar">
              <span className="spacer"/>
              <RefreshButton label="Reload" onRefresh={showLog}/>
            </div>
            <pre className="logs">{log || t('sandboxes.noOutput')}</pre>
          </>
        )}
        {tab === 'env' && <pre className="code">{box.command ? `$ ${box.command}\n\n` : ''}
          {envToLines(box.env) || '—'}</pre>}
      </div>
    </Card>
  )
}

function short(msg: string): string {
  return msg.length > 48 ? msg.slice(0, 48) + '…' : msg
}

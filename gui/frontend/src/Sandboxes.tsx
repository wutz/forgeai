import {useEffect, useRef, useState} from 'react'
import {
  DeleteSandbox, GetSandbox, KeepSandboxAwake, ListSandboxes, SandboxLogs, StartSandbox, StopSandbox,
} from '../wailsjs/go/app/App'
import {app, sandbox} from '../wailsjs/go/models'
import {SandboxCreateDialog} from './SandboxCreateDialog'
import {SandboxTemplateDialog} from './SandboxTemplates'
import {SandboxTerminal} from './SandboxTerminal'
import {Status, envToLines, fmtDate, errText} from './util'
import {Badge, Card, Empty, Icon, LogView, PageHeader, Spinner, Tabs, tabPanelProps, useActive, useConfirm} from './ui'
import {MessageKey, t} from './i18n'

// HEARTBEAT is how often an open sandbox detail resets the sandbox's idle
// clock. The operator suspends a sandbox once it has been idle for its idle
// timeout, so the beats stop when the detail closes.
const HEARTBEAT = 60_000

// Sandboxes is the view of the project's agent sandboxes (agents.x-k8s.io).
export function Sandboxes() {
  // boxes is null until the first read, so a failed first read is not shown as an empty list.
  const [boxes, setBoxes] = useState<app.Sandbox[] | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const active = useActive()

  // listed holds the names of the last completed list. A selected sandbox that
  // was listed and is now gone was deleted elsewhere, so its detail is closed.
  // A sandbox just created is not listed yet and keeps its detail.
  const listed = useRef(new Set<string>())
  // Each refresh takes a number; a response that is not from the latest refresh
  // is dropped, so a slow older poll cannot overwrite a newer list.
  const latest = useRef(0)
  const refresh = () => {
    const seq = ++latest.current
    ListSandboxes().then(bs => {
      if (seq !== latest.current) return
      const names = new Set(bs.map(b => b.name))
      const before = listed.current
      listed.current = names
      setBoxes(bs)
      setSelected(cur => cur && before.has(cur) && !names.has(cur) ? null : cur)
      setError('')
    }).catch(e => {
      if (seq === latest.current) setError(errText(e))
    })
  }
  useEffect(() => {
    if (!active) return
    refresh()
    const timer = setInterval(refresh, 5000)
    return () => clearInterval(timer)
  }, [active])

  // Same definition as the detail view: a Pending sandbox is on its way up.
  const list = boxes ?? []
  const running = list.filter(b => b.phase === 'Running' || b.phase === 'Pending').length

  return (
    <div className="page">
      <PageHeader eyebrow={t('sandboxes.eyebrow')} title={t('sandboxes.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {t('sandboxes.runningCount', {running, total: list.length})}
                  </span>}
                  actions={<button className="btn" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>{t('sandboxes.new')}
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && <SandboxCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}
      <div className="split">
        <SandboxTable boxes={boxes} failed={!!error} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {/* Keyed by name: a read still in flight for the previous sandbox must not land on this detail, where Stop and Delete would act on it. */}
        {selected && <SandboxDetail key={selected} name={selected} onDone={refresh}
                                    onDeleted={() => { setSelected(null); refresh() }}/>}
      </div>
    </div>
  )
}

function SandboxTable({boxes, failed, selected, onSelect}: {
  boxes: app.Sandbox[] | null, failed: boolean, selected: string | null, onSelect: (name: string) => void
}) {
  if (!boxes) return <Card>{failed ? null : <Spinner label={t('common.loading')}/>}</Card>
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
          <tr key={b.name} className={selected === b.name ? 'clickable selected' : 'clickable'}
              onClick={() => onSelect(b.name)}>
            <td className="mono">
              <button type="button" className="row-name cell-main">{b.name}</button>
              <span className="cell-sub">{b.image.split('/').pop()}</span>
            </td>
            {/* A stopped sandbox's controller message is English and repeats the status label. */}
            <td><Status phase={b.phase}/>{b.phase !== 'Running' && !b.stopped && statusText(b).label &&
              <div className="cell-sub" title={b.message}>{short(statusText(b).label)}</div>}</td>
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
  // error is an action's failure; loadError is a failed read. A read clears only
  // its own error, so a failed Stop stays shown until the next action.
  const [error, setError] = useState('')
  const [loadError, setLoadError] = useState('')
  const [tab, setTab] = useState<Tab>('overview')
  // null until the first read, so the view can tell "reading" from "no output".
  const [log, setLog] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [savedTemplate, setSavedTemplate] = useState('')
  // pending is the transition in flight. It stays set until the phase reaches
  // its target, because suspending or resuming takes the controller a while.
  const [pending, setPending] = useState<'start' | 'stop' | null>(null)
  // deleting is the delete call in flight; the view closes once it succeeds.
  const [deleting, setDeleting] = useState(false)
  const active = useActive()
  const confirm = useConfirm()
  const phase = box?.phase
  // reload reads the sandbox now, so an action's result shows without waiting for the poll.
  const reload = () => GetSandbox(name).then(b => {
    setBox(b)
    setLoadError('')
  }).catch(e => setLoadError(errText(e)))
  const transition = (kind: 'start' | 'stop', p: Promise<unknown>) => {
    setPending(kind)
    setError('')
    p.then(() => { onDone(); return reload() }).catch(e => {
      setError(errText(e))
      setPending(null)
    })
  }

  useEffect(() => {
    if (!phase || !pending) return
    // A start is done once the sandbox leaves Stopped, even into Failed or
    // Expired; a stop is done once it reaches a settled phase.
    const settled = pending === 'start'
      ? phase !== 'Stopped'
      : phase === 'Stopped' || phase === 'Failed' || phase === 'Expired' || phase === 'Succeeded'
    if (settled) setPending(null)
  }, [phase, pending])

  useEffect(() => {
    setBox(null)
    setLog(null)
    setTab('overview')
    setPending(null)
    setDeleting(false)
    setSaving(false)
    setSavedTemplate('')
    setLoadError('')
  }, [name])

  // The poll and heartbeat run only while the view is on screen: a hidden
  // detail lets the sandbox go idle, as it does when the detail closes.
  useEffect(() => {
    if (!active) return
    reload()
    const timer = setInterval(reload, 5000)
    // Keep the sandbox awake while its detail is open and running.
    const beat = setInterval(() => KeepSandboxAwake(name).catch(() => {}), HEARTBEAT)
    KeepSandboxAwake(name).catch(() => {})
    return () => {
      clearInterval(timer)
      clearInterval(beat)
    }
  }, [name, active])

  const failure = error || loadError
  if (!box) return <Card>{failure
    ? <div className="banner banner-error" role="alert">{failure}</div>
    : <Spinner label={t('sandboxes.loading')}/>}</Card>
  const running = box.phase === 'Running' || box.phase === 'Pending'
  const busy = pending !== null || deleting
  // showLog rejects when the logs cannot be read; the banner shows why.
  const showLog = () =>
    SandboxLogs(box.name, 500).then(v => { setLog(v); setTab('log') }, e => {
      setError(errText(e))
      throw e
    })

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h2 className="mono">{box.name}</h2>
          <div className="card-sub">{box.runtimeClass || 'runc'} · {t('sandboxes.createdAt', {date: fmtDate(box.created)})}</div>
        </div>
        <div className="card-head-actions"><Status phase={box.phase}/></div>
      </div>
      {failure && <div className="banner banner-error" role="alert">{failure}</div>}
      {savedTemplate && <div className="banner" role="status">{t('sandboxes.savedTemplate', {name: savedTemplate})}</div>}
      {confirm.dialog}
      {saving && <SandboxTemplateDialog initial={sandboxTemplateForm(box)} onClose={() => setSaving(false)}
                                        onSaved={n => { setSaving(false); setSavedTemplate(n) }}/>}

      <div className="detail-actions">
        <button className="btn btn-sm" onClick={() => showLog().catch(() => {})}>
          <Icon name="scroll" size={13}/>{t('common.logs')}
        </button>
        {running && <button className="btn btn-sm" disabled={busy} onClick={() => confirm.ask({
          title: t('sandboxes.stopTitle', {name: box.name}),
          message: t('sandboxes.stopMessage'),
          action: t('common.stop'),
          run: () => transition('stop', StopSandbox(box.name)),
        })}>
          {pending === 'stop' ? <span className="spinner" aria-hidden/> : <Icon name="square" size={12}/>}
          {pending === 'stop' ? t('sandboxes.stopping') : t('common.stop')}
        </button>}
        {!running && <button className="btn btn-sm btn-accent" disabled={busy}
                             onClick={() => transition('start', StartSandbox(box.name))}>
          {pending === 'start' ? <span className="spinner" aria-hidden/> : <Icon name="play" size={12}/>}
          {pending === 'start' ? t('sandboxes.starting') : t('common.start')}
        </button>}
        <button className="btn btn-sm" onClick={() => setSaving(true)}>
          <Icon name="layers" size={13}/>{t('sandboxes.saveAsTemplate')}
        </button>
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" disabled={busy} onClick={() => confirm.ask({
          title: t('sandboxes.deleteTitle', {name: box.name}),
          message: t('sandboxes.deleteMessage'),
          action: t('sandboxes.deleteAction'), danger: true,
          run: () => {
            setDeleting(true)
            setError('')
            DeleteSandbox(box.name).then(onDeleted).catch(e => setError(errText(e))).finally(() => setDeleting(false))
          },
        })}><Icon name="trash" size={13}/>{deleting ? t('sandboxes.deleting') : t('common.delete')}</button>
      </div>

      <div className="section">
        <Tabs idPrefix="sandbox" tabs={[
          {value: 'overview', label: t('sandboxes.tabOverview'), icon: 'grid'},
          {value: 'terminal', label: t('sandboxes.tabTerminal'), icon: 'terminal'},
          {value: 'log', label: t('sandboxes.tabLog'), icon: 'scroll'},
          {value: 'env', label: t('sandboxes.tabEnv'), icon: 'layers'},
        ]} value={tab} onChange={v => {
          setTab(v)
          // The log tab shows what was last read, so opening it reads the logs.
          if (v === 'log') showLog().catch(() => {})
        }}/>

        <div {...tabPanelProps('sandbox', tab)}>
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
              {!box.stopped && statusText(box).label && <>
                <dt>{t('common.status')}</dt>
                <dd>{statusText(box).label}
                  {statusText(box).detail && <div className="cell-sub mono">{statusText(box).detail}</div>}
                </dd>
              </>}
              <dt>{t('common.created')}</dt><dd>{fmtDate(box.created)}</dd>
            </dl>
          )}
          {tab === 'terminal' && (box.podPhase === 'Running' && box.phase !== 'Stopping'
            ? <SandboxTerminal name={box.name}/>
            // The idle box is the terminal's size, so opening it does not shift the layout.
            : <div className="term term-idle">
                <span role="status">
                  {box.phase === 'Pending' || box.phase === 'Starting' || box.podPhase === 'Pending'
                    ? t('sandboxes.terminalStarting')
                    : t('sandboxes.startToOpenTerminal')}
                </span>
                {!running && <button className="btn btn-sm btn-accent" disabled={busy}
                                     onClick={() => transition('start', StartSandbox(box.name))}>
                  {pending === 'start' ? <span className="spinner" aria-hidden/> : <Icon name="play" size={12}/>}
                  {pending === 'start' ? t('sandboxes.starting') : t('common.start')}
                </button>}
              </div>)}
          {tab === 'log' && <LogView text={log} onRefresh={showLog}/>}
          {tab === 'env' && <pre className="code">{box.command ? `$ ${box.command}\n\n` : ''}
            {envToLines(box.env) || '—'}</pre>}
        </div>
      </div>
    </Card>
  )
}

function short(msg: string): string {
  return msg.length > 48 ? msg.slice(0, 48) + '…' : msg
}

// reasonKeys maps the reasons the backend sends (sandbox.PhaseReason) to their
// copy. A reason without copy here shows the controller's message as written.
const reasonKeys: Record<string, MessageKey> = {
  AwaitingController: 'sandboxes.reason.awaitingController',
  DependenciesNotReady: 'sandboxes.reason.dependenciesNotReady',
  PodFailed: 'sandboxes.reason.podFailed',
  PodSucceeded: 'sandboxes.reason.podSucceeded',
  InvalidConfiguration: 'sandboxes.reason.invalidConfiguration',
  ReconcilerError: 'sandboxes.reason.reconcilerError',
  MultiplePods: 'sandboxes.reason.multiplePods',
  SandboxExpired: 'sandboxes.reason.expired',
}

// statusText says why a sandbox is not running: label is the line to show and
// detail the controller's message under it. A reason with copy is worded in the
// UI language and its message is the detail; any other message is the label.
function statusText(b: app.Sandbox): {label: string, detail: string} {
  const key = b.reason ? reasonKeys[b.reason] : undefined
  if (key) return {label: t(key), detail: b.message ?? ''}
  return {label: b.message ?? '', detail: ''}
}

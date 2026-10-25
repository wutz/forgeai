import {useEffect, useRef, useState} from 'react'
import {
  DeleteSandbox, GetSandbox, KeepSandboxAwake, ListSandboxes, SandboxLogs, StartSandbox, StopSandbox,
} from '../wailsjs/go/app/App'
import {app, sandbox} from '../wailsjs/go/models'
import {SandboxCreateDialog} from './SandboxCreateDialog'
import {SandboxTemplateDialog} from './SandboxTemplates'
import {SandboxTerminal} from './SandboxTerminal'
import {Templates} from './Templates'
import {Status, envToLines, everyWhileVisible, fmtDate, errText} from './util'
import {Badge, Card, Empty, Icon, LogView, Pane, PageHeader, Spinner, Tabs, tabPanelProps, useActive, useConfirm, useDetailClose} from './ui'
import {MessageKey, t} from './i18n'

// HEARTBEAT is how often an open sandbox detail resets the sandbox's idle
// clock. The operator suspends a sandbox once it has been idle for its idle
// timeout, so the beats stop when the detail closes.
const HEARTBEAT = 60_000

// TRANSITION_TIMEOUT_MS bounds a start or stop wait. Suspending or resuming takes
// the controller a while, but a wait past this is a stuck transition, not a slow one.
const TRANSITION_TIMEOUT_MS = 120_000

type Section = 'sandboxes' | 'templates'

// Sandboxes is the view of the project's agent sandboxes (agents.x-k8s.io). The
// sandbox templates it starts new sandboxes from open from a button in its top bar.
export function Sandboxes() {
  const [section, setSection] = useState<Section>('sandboxes')
  return (
    <div className="page">
      <Pane on={section === 'sandboxes'}><SandboxList onTemplates={() => setSection('templates')}/></Pane>
      <Pane on={section === 'templates'}><Templates kind="sandbox" onBack={() => setSection('sandboxes')}/></Pane>
    </div>
  )
}

function SandboxList({onTemplates}: {onTemplates: () => void}) {
  // boxes is null until the first read, so a failed first read is not shown as an empty list.
  const [boxes, setBoxes] = useState<app.Sandbox[] | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const {split, close: closeDetail} = useDetailClose(selected, setSelected)
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
    return everyWhileVisible(refresh, 5000)
  }, [active])

  // Same definition as the detail view: a Pending sandbox is on its way up.
  const list = boxes ?? []
  const running = list.filter(b => b.phase === 'Running' || b.phase === 'Pending').length

  return (
    <>
      <PageHeader eyebrow={t('sandboxes.eyebrow')} title={t('sandboxes.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {boxes !== null && t('sandboxes.runningCount', {running, total: list.length})}
                  </span>}
                  actions={<>
                    <button className="btn btn-ghost" onClick={onTemplates}>
                      <Icon name="layers" size={14}/>{t('sandboxes.templates')}
                    </button>
                    <button className="btn" onClick={() => setCreating(true)}>
                      <Icon name="plus" size={14}/>{t('sandboxes.new')}
                    </button>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && <SandboxCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}
      <div className="split" ref={split}>
        <SandboxTable boxes={boxes} failed={!!error} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {/* Keyed by name: a read still in flight for the previous sandbox must not land on this detail, where Stop and Delete would act on it. */}
        {selected && <SandboxDetail key={selected} name={selected} onDone={refresh}
                                    onDeleted={() => { setSelected(null); refresh() }} onClose={closeDetail}/>}
      </div>
    </>
  )
}

function SandboxTable({boxes, failed, selected, onSelect}: {
  boxes: app.Sandbox[] | null, failed: boolean, selected: string | null, onSelect: (name: string) => void
}) {
  // A failed first read leaves nothing to list, so the card is left out under the banner.
  if (!boxes) return failed ? null : <Card><Spinner label={t('common.loading')}/></Card>
  if (!boxes.length) {
    return <Card><Empty icon="box" title={t('sandboxes.emptyTitle')}>
      {t('sandboxes.emptyHint')}</Empty></Card>
  }
  return (
    <Card>
      {/* Beside an open sandbox the table drops isolation and age: the detail shows both. */}
      <table className="table sandbox-table">
        <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('common.status')}</th>
          {!selected && <th>{t('sandboxes.isolation')}</th>}
          <th>{t('sandboxes.resources')}</th>
          <th>{t('sandboxes.endpoint')}</th>
          {!selected && <th>{t('sandboxes.age')}</th>}
        </tr>
        </thead>
        <tbody>
        {boxes.map(b => (
          <tr key={b.name} className={selected === b.name ? 'clickable selected' : 'clickable'}
              onClick={() => onSelect(b.name)}>
            <td data-label={t('common.name')} className="mono">
              <button type="button" className="row-name cell-main">{b.name}</button>
              <span className="cell-sub">{b.image.split('/').pop()}</span>
            </td>
            {/* A stopped sandbox's controller message is English and repeats the status label. */}
            <td data-label={t('common.status')}><Status phase={b.phase}/>{b.phase !== 'Running' && !b.stopped && statusText(b).label &&
              <div className="cell-sub" title={b.message}>{short(statusText(b).label)}</div>}
              {b.phase === 'Running' && containerNote(b) &&
              <div className="cell-sub" title={containerNote(b)}>{short(containerNote(b))}</div>}</td>
            {!selected && <td data-label={t('sandboxes.isolation')}>{b.runtimeClass ? <Badge>{b.runtimeClass}</Badge> : <span className="muted">{t('sandboxes.runcNode')}</span>}</td>}
            <td data-label={t('sandboxes.resources')} className="mono">{b.cpu || '—'}/{b.memory || '—'}{b.gpu ? ` · ${b.gpu} GPU` : ''}</td>
            <td data-label={t('sandboxes.endpoint')} className="mono trunc" title={b.gatewayURL || b.serviceFQDN || b.podIPs?.[0]}>{b.gatewayURL || b.serviceFQDN || b.podIPs?.[0] || '—'}</td>
            {!selected && <td data-label={t('sandboxes.age')} className="muted">{b.age}</td>}
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

function SandboxDetail({name, onDone, onDeleted, onClose}: {
  name: string, onDone: () => void, onDeleted: () => void, onClose: () => void
}) {
  const [box, setBox] = useState<app.Sandbox | null>(null)
  // error is an action's failure; loadError is a failed read. A read clears only
  // its own error, so a failed Stop stays shown until the next action.
  const [error, setError] = useState('')
  const [loadError, setLoadError] = useState('')
  // heartbeatError is the last failed keep-awake call; the next successful beat clears it.
  const [heartbeatError, setHeartbeatError] = useState('')
  // logError is a failed log read; a successful read clears it, while an action's error stays.
  const [logError, setLogError] = useState('')
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
  // Reads are latest-wins: a slow poll that started before an action cannot
  // overwrite the state read after it.
  const reloadSeq = useRef(0)
  const reload = () => {
    const seq = ++reloadSeq.current
    return GetSandbox(name).then(b => {
      if (seq !== reloadSeq.current) return
      setBox(b)
      setLoadError('')
    }).catch(e => {
      if (seq === reloadSeq.current) setLoadError(errText(e))
    })
  }
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
    // A start is done once the sandbox leaves Stopped and Expired (a start clears
    // the shutdown time, so Expired is left behind too), even into Failed; a stop
    // is done once it reaches a settled phase.
    const settled = pending === 'start'
      ? phase !== 'Stopped' && phase !== 'Expired'
      : phase === 'Stopped' || phase === 'Failed' || phase === 'Expired' || phase === 'Succeeded'
    if (settled) setPending(null)
  }, [phase, pending])

  // A transition the controller never finishes must not hold the buttons forever:
  // after the timeout the view releases them and says so.
  useEffect(() => {
    if (!pending) return
    const timer = window.setTimeout(() => {
      setPending(null)
      setError(t(pending === 'start' ? 'sandboxes.startTimeout' : 'sandboxes.stopTimeout'))
    }, TRANSITION_TIMEOUT_MS)
    return () => window.clearTimeout(timer)
  }, [pending])

  useEffect(() => {
    setBox(null)
    setLog(null)
    setLogError('')
    setTab('overview')
    setPending(null)
    setDeleting(false)
    setSaving(false)
    setSavedTemplate('')
    setLoadError('')
    setHeartbeatError('')
  }, [name])

  // The poll runs only while the view is on screen: a hidden detail lets the
  // sandbox go idle, as it does when the detail closes.
  useEffect(() => {
    if (!active) return
    reload()
    return everyWhileVisible(reload, 5000)
  }, [name, active])

  // The heartbeat keeps a running sandbox from going idle. A stopped one is left alone.
  // A failed beat is shown: the sandbox may then go idle and be suspended without the user knowing.
  const running = box?.phase === 'Running' || box?.phase === 'Pending'
  useEffect(() => {
    if (!active || !running) return
    const beat = () => KeepSandboxAwake(name).then(() => setHeartbeatError(''), e => setHeartbeatError(errText(e)))
    beat()
    return everyWhileVisible(beat, HEARTBEAT)
  }, [name, active, running])

  const failure = error || loadError
  if (!box) return <Card>{failure
    ? <div className="banner banner-error" role="alert">{failure}</div>
    : <Spinner label={t('sandboxes.loading')}/>}</Card>
  const busy = pending !== null || deleting
  // showLog rejects when the logs cannot be read; the log tab shows why.
  const showLog = () =>
    SandboxLogs(box.name, 500).then(v => { setLog(v); setLogError(''); setTab('log') }, e => {
      setLogError(errText(e))
      throw e
    })
  // Selecting the log tab loads the logs, since the tab is the only entry point.
  // The tab opens at once, so a failed read is shown in it rather than hidden.
  const selectTab = (next: Tab) => {
    setTab(next)
    if (next === 'log') showLog().catch(() => {})
  }

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h2 className="mono">{box.name}</h2>
          <div className="card-sub">{box.runtimeClass || 'runc'} · {t('sandboxes.createdAt', {date: fmtDate(box.created)})}</div>
        </div>
        <div className="card-head-actions">
          <Status phase={box.phase}/>
          <button type="button" className="icon-btn" aria-label={t('common.close')} title={t('common.close')} onClick={onClose}>
            <Icon name="close" size={14}/>
          </button>
        </div>
      </div>
      {failure && <div className="banner banner-error" role="alert">{failure}</div>}
      {running && heartbeatError && <div className="banner banner-warn" role="status">{t('sandboxes.heartbeatFailed', {error: heartbeatError})}</div>}
      {savedTemplate && <div className="banner" role="status">{t('sandboxes.savedTemplate', {name: savedTemplate})}</div>}
      {confirm.dialog}
      {saving && <SandboxTemplateDialog initial={sandboxTemplateForm(box)} onClose={() => setSaving(false)}
                                        onSaved={n => { setSaving(false); setSavedTemplate(n) }}/>}

      <div className="detail-actions">
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
        ]} value={tab} onChange={selectTab}/>

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
              {!!box.gatewayPort && <><dt>{t('sandboxes.publicUrl')}</dt><dd className="mono">
                {box.gatewayURL
                  ? <PublicURL url={box.gatewayURL}/>
                  : t('sandboxes.gatewayUnreachable', {port: box.gatewayPort})}
              </dd></>}
              {box.ssh && <><dt>{t('sandboxes.ssh')}</dt><dd className="mono">{box.sshEndpoint}
                <div className="cell-sub">{t('sandboxes.sshHint')}</div></dd></>}
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
              {box.phase === 'Running' && containerNote(box) && <>
                <dt>{t('common.status')}</dt>
                <dd>{containerNote(box)}</dd>
              </>}
              <dt>{t('common.created')}</dt><dd>{fmtDate(box.created)}</dd>
            </dl>
          )}
          {tab === 'terminal' && ((box.containerRunning || box.podUnknown)
            ? <SandboxTerminal name={box.name}/>
            // The idle box is the terminal's size, so opening it does not shift the layout.
            : <div className="term term-idle">
                <span role="status">
                  {containerNote(box) || (box.phase === 'Pending' || box.podPhase === 'Pending'
                      ? t('sandboxes.terminalStarting')
                      : t('sandboxes.startToOpenTerminal'))}
                </span>
                {!running && <button className="btn btn-sm btn-accent" disabled={busy}
                                     onClick={() => transition('start', StartSandbox(box.name))}>
                  {pending === 'start' ? <span className="spinner" aria-hidden/> : <Icon name="play" size={12}/>}
                  {pending === 'start' ? t('sandboxes.starting') : t('common.start')}
                </button>}
              </div>)}
          {tab === 'log' && <>
            {logError && <div className="banner banner-error" role="alert">
              <span>{logError}</span>
              {/* With no log read yet, the log view would wait on a read that failed; retry offers the read again. */}
              {log === null && <>
                <span className="spacer"/>
                <button className="btn btn-sm" onClick={() => showLog().catch(() => {})}>{t('common.retry')}</button>
              </>}
            </div>}
            {log === null && logError ? null : <LogView text={log} onRefresh={showLog}/>}
          </>}
          {tab === 'env' && <pre className="code">{box.command ? `$ ${box.command}\n\n` : ''}
            {envToLines(box.env) || '—'}</pre>}
        </div>
      </div>
    </Card>
  )
}

// PublicURL is the address a published sandbox is reached at, with a copy button.
function PublicURL({url}: {url: string}) {
  const [copied, setCopied] = useState(false)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const copy = () => {
    navigator.clipboard.writeText(url).then(() => {
      setCopied(true)
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setCopied(false), 1500)
    }, () => {})
  }
  return <>
    {url}
    <button type="button" className="btn btn-sm btn-ghost" onClick={copy}>
      <Icon name={copied ? 'check' : 'copy'} size={13}/>{copied ? t('common.copied') : t('common.copy')}
    </button>
  </>
}

function short(msg: string): string {
  return msg.length > 48 ? msg.slice(0, 48) + '…' : msg
}

// containerNote says why the sandbox's container is not running, or is empty
// while it is starting. The pod can stay Running while its container crash-loops,
// so the list and overview show this too, not only the terminal.
function containerNote(b: app.Sandbox): string {
  if (!b.containerReason || ['ContainerCreating', 'PodInitializing'].includes(b.containerReason)) return ''
  return t('sandboxes.containerNotRunning', {reason: b.containerReason})
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

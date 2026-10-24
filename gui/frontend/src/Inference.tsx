import {useEffect, useRef, useState} from 'react'
import {
  DeleteInferenceService, GetInferenceService, InferenceAPIKey, ListInferenceServices, StartInferenceService, StopInferenceService,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {InferenceCreateDialog} from './InferenceCreateDialog'
import {errText, everyWhileVisible, fmtDate} from './util'
import {Card, Empty, Icon, PageHeader, Spinner, useActive, useConfirm, useDetailClose} from './ui'
import {MessageKey, t} from './i18n'

// Inference is the view of the project's InferenceServices: each serves one
// model behind an OpenAI-compatible endpoint, reached with its API key.
export function Inference() {
  // services is null until the first read, so a failed first read is not shown as an empty list.
  const [services, setServices] = useState<app.InferenceService[] | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const {split, close: closeDetail} = useDetailClose(selected, setSelected)
  const [creating, setCreating] = useState(false)
  const active = useActive()

  // listed holds the names of the last completed list. A selected service that
  // was listed and is now gone was deleted elsewhere, so its detail is closed.
  // A service just created is not listed yet and keeps its detail.
  const listed = useRef(new Set<string>())
  // Each refresh takes a number; a response from an older refresh is dropped.
  const latest = useRef(0)
  const refresh = () => {
    const seq = ++latest.current
    ListInferenceServices().then(ss => {
      if (seq !== latest.current) return
      const names = new Set(ss.map(s => s.name))
      const before = listed.current
      listed.current = names
      setServices(ss)
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

  const list = services ?? []
  const ready = list.filter(s => s.ready).length

  return (
    <div className="page">
      <PageHeader eyebrow={t('inference.eyebrow')} title={t('inference.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {services !== null && t('inference.readyCount', {ready, total: list.length})}
                  </span>}
                  actions={<button className="btn" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>{t('inference.new')}
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && <InferenceCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}
      <div className="split" ref={split}>
        <ServiceTable services={services} failed={!!error} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {/* Keyed by name: a read still in flight for the previous service must not land on this detail. */}
        {selected && <InferenceDetail key={selected} name={selected} onChanged={refresh}
                                      onDeleted={() => { setSelected(null); refresh() }} onClose={closeDetail}/>}
      </div>
    </div>
  )
}

function ServiceTable({services, failed, selected, onSelect}: {
  services: app.InferenceService[] | null, failed: boolean, selected: string | null, onSelect: (name: string) => void
}) {
  // A failed first read leaves nothing to list, so the card is left out under the banner.
  if (!services) return failed ? null : <Card><Spinner label={t('common.loading')}/></Card>
  if (!services.length) {
    return <Card><Empty icon="bolt" title={t('inference.emptyTitle')}>
      {t('inference.emptyHint')}</Empty></Card>
  }
  return (
    <Card>
      {/* Beside an open service the table drops the age: the detail shows it. */}
      <table className="table">
        <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('common.status')}</th>
          <th>{t('inference.colReplicas')}</th>
          <th>{t('inference.colEndpoint')}</th>
          {!selected && <th>{t('inference.colAge')}</th>}
        </tr>
        </thead>
        <tbody>
        {services.map(s => (
          <tr key={s.name} className={selected === s.name ? 'clickable selected' : 'clickable'}
              onClick={() => onSelect(s.name)}>
            <td data-label={t('common.name')} className="mono">
              <button type="button" className="row-name cell-main">{s.name}</button>
              <span className="cell-sub">{s.model}</span>
            </td>
            <td data-label={t('common.status')}><ServiceStatus service={s}/></td>
            <td data-label={t('inference.colReplicas')} className="mono">{s.readyReplicas}/{s.replicas}</td>
            <td data-label={t('inference.colEndpoint')} className="mono trunc" title={s.endpoint}>{s.endpoint || '—'}</td>
            {!selected && <td data-label={t('inference.colAge')} className="muted">{s.age}</td>}
          </tr>
        ))}
        </tbody>
      </table>
    </Card>
  )
}

// ServiceStatus names the state of a service from its Ready condition. A
// service the controller has not reconciled yet is still starting.
function ServiceStatus({service: s}: {service: app.InferenceService}) {
  const state = s.stopped ? 'stopped'
    : s.ready ? 'ready'
      : s.reason === 'Starting' || !s.reason ? 'starting'
        : 'failed'
  const labels: Record<string, MessageKey> = {
    stopped: 'inference.stateStopped', ready: 'inference.stateReady',
    starting: 'inference.stateStarting', failed: 'inference.stateFailed',
  }
  // The dot reuses the phase colours: ready is running, starting is pending.
  const phase = {stopped: 'suspended', ready: 'running', starting: 'starting', failed: 'failed'}[state]
  return (
    <>
      <span className={`status status-${phase}`}><span className="dot" aria-hidden/>{t(labels[state])}</span>
      {state === 'failed' && s.message && <div className="cell-sub" title={s.message}>{s.message}</div>}
    </>
  )
}

function InferenceDetail({name, onChanged, onDeleted, onClose}: {
  name: string, onChanged: () => void, onDeleted: () => void, onClose: () => void
}) {
  // svc is null until the first read; a failed read says so instead of showing nothing.
  const [svc, setSvc] = useState<app.InferenceService | null>(null)
  const [loadError, setLoadError] = useState('')
  // error is an action's failure. A read clears only its own error.
  const [error, setError] = useState('')
  // pending is the start or stop call in flight.
  const [pending, setPending] = useState<'start' | 'stop' | null>(null)
  const [deleting, setDeleting] = useState(false)
  // key is the API key once the user shows it. It lives only as long as this detail.
  const [key, setKey] = useState<string | null>(null)
  const [keyError, setKeyError] = useState('')
  const [copied, setCopied] = useState('')
  const copyTimer = useRef(0)
  const latest = useRef(0)
  const active = useActive()
  const confirm = useConfirm()

  useEffect(() => () => window.clearTimeout(copyTimer.current), [])

  // reload reads the service now, so an action's result shows without waiting for the poll.
  // Reads are latest-wins, so a slow poll cannot overwrite a newer read.
  const reload = () => {
    const seq = ++latest.current
    return GetInferenceService(name).then(s => {
      if (seq !== latest.current) return
      setSvc(s)
      setLoadError('')
    }).catch(e => {
      if (seq === latest.current) setLoadError(errText(e))
    })
  }
  useEffect(() => {
    if (!active) return
    reload()
    return everyWhileVisible(reload, 5000)
  }, [active, name])

  const transition = (kind: 'start' | 'stop', call: Promise<app.InferenceService>) => {
    setPending(kind)
    setError('')
    return call.then(s => {
      setSvc(s)
      onChanged()
    }).catch(e => setError(errText(e))).finally(() => setPending(null))
  }

  const reveal = () => {
    setKeyError('')
    InferenceAPIKey(name).then(setKey).catch(e => setKeyError(errText(e)))
  }

  const copy = (text: string, what: string) =>
    navigator.clipboard.writeText(text).then(() => {
      setCopied(what)
      window.clearTimeout(copyTimer.current)
      copyTimer.current = window.setTimeout(() => setCopied(''), 1500)
    }, () => setError(t('images.copyFailed')))

  if (!svc) {
    return <Card className="detail">
      {loadError
        ? <div className="banner banner-error" role="alert">{loadError}</div>
        : <Spinner label={t('common.loading')}/>}
    </Card>
  }
  const busy = pending !== null || deleting
  const endpoint = svc.endpoint || ''
  // The example is what a client sends; the key stays a shell variable until shown.
  const example = [
    `curl ${endpoint}/chat/completions \\`,
    '  -H "Authorization: Bearer $API_KEY" \\',
    '  -H "Content-Type: application/json" \\',
    `  -d '{"model": "${svc.servedName}", "messages": [{"role": "user", "content": "Hello"}]}'`,
  ].join('\n')

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h2 className="mono">{svc.name}</h2>
          <div className="card-sub">{svc.model} · {t('inference.created')} {fmtDate(svc.created)}</div>
        </div>
        <div className="card-head-actions">
          <ServiceStatus service={svc}/>
          <button type="button" className="icon-btn" aria-label={t('common.close')} title={t('common.close')} onClick={onClose}>
            <Icon name="close" size={14}/>
          </button>
        </div>
      </div>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      <div className="detail-actions">
        {!svc.stopped && <button className="btn btn-sm" disabled={busy} onClick={() => confirm.ask({
          title: t('inference.stopTitle', {name: svc.name}),
          message: t('inference.stopMessage'),
          action: t('inference.stopAction'),
          run: () => transition('stop', StopInferenceService(svc.name)),
        })}>
          {pending === 'stop' ? <span className="spinner" aria-hidden/> : <Icon name="square" size={12}/>}
          {pending === 'stop' ? t('inference.stopping') : t('common.stop')}
        </button>}
        {svc.stopped && <button className="btn btn-sm btn-accent" disabled={busy}
                                onClick={() => transition('start', StartInferenceService(svc.name))}>
          {pending === 'start' ? <span className="spinner" aria-hidden/> : <Icon name="play" size={12}/>}
          {pending === 'start' ? t('inference.starting') : t('common.start')}
        </button>}
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" disabled={busy} onClick={() => confirm.ask({
          title: t('inference.deleteTitle', {name: svc.name}),
          message: t('inference.deleteMessage'),
          action: t('inference.deleteAction'), danger: true,
          run: () => {
            setDeleting(true)
            setError('')
            return DeleteInferenceService(svc.name).then(onDeleted).catch(e => setError(errText(e)))
              .finally(() => setDeleting(false))
          },
        })}><Icon name="trash" size={13}/>{deleting ? t('inference.deleting') : t('common.delete')}</button>
      </div>

      <div className="section">
        <dl className="kv">
          <dt>{t('inference.servedAs')}</dt><dd className="mono">{svc.servedName}</dd>
          <dt>{t('inference.instance')}</dt><dd className="mono">{svc.instanceType}</dd>
          <dt>{t('inference.replicas')}</dt><dd className="mono">{svc.readyReplicas}/{svc.replicas}</dd>
          {svc.args && svc.args.length > 0 && <><dt>{t('inference.args')}</dt><dd className="mono">{svc.args.join(' ')}</dd></>}
          {svc.huggingFaceSecret && <><dt>{t('inference.hfSecret')}</dt><dd className="mono">{svc.huggingFaceSecret}</dd></>}
          <dt>{t('inference.endpoint')}</dt>
          <dd className="mono">
            {endpoint || '—'}
            {endpoint && <button type="button" className="btn btn-sm btn-ghost" onClick={() => copy(endpoint, 'endpoint')}>
              <Icon name={copied === 'endpoint' ? 'check' : 'copy'} size={13}/>
              {copied === 'endpoint' ? t('common.copied') : t('common.copy')}
            </button>}
          </dd>
        </dl>
      </div>

      <div className="section">
        <h3>{t('inference.apiKey')}</h3>
        {keyError && <div className="banner banner-error" role="alert">{keyError}</div>}
        {key === null
          ? <button type="button" className="btn btn-sm" disabled={!svc.apiKeySecret} onClick={reveal}>
            <Icon name="shield" size={13}/>{t('inference.showKey')}
          </button>
          : <div style={{display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap'}}>
            <code className="mono trunc">{key}</code>
            <button type="button" className="btn btn-sm btn-ghost" onClick={() => copy(key, 'key')}>
              <Icon name={copied === 'key' ? 'check' : 'copy'} size={13}/>
              {copied === 'key' ? t('common.copied') : t('common.copy')}
            </button>
            <button type="button" className="btn btn-sm btn-ghost" onClick={() => setKey(null)}>{t('inference.hideKey')}</button>
          </div>}
        {!svc.apiKeySecret && <div className="cell-sub">{t('inference.keyUnavailable')}</div>}
      </div>

      {endpoint && <div className="section">
        <h3>{t('inference.example')}</h3>
        <pre className="code">{example}</pre>
        <div className="cell-sub">{t('inference.exampleHint')}</div>
      </div>}
    </Card>
  )
}

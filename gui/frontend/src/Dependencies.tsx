import {Fragment, useEffect, useRef, useState} from 'react'
import {InstallDependency, ListDependencies} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Badge, Card, Empty, Icon, Meter, PageHeader, RefreshButton, Spinner, useActive, useConfirm} from './ui'
import {MessageKey, getLocale, t} from './i18n'

// Progress is one deps:progress event: a step of an install, or its outcome
// when done is set (app.DependencyProgress). Wails only generates models for
// bound methods, so the event payload is typed here.
type Progress = {cluster: string, name: string, step?: string, done: boolean, error?: string}

// Dependencies installs and checks the cluster components forgeai runs on:
// Kubeflow Trainer v2 with JobSet, Kueue and agent-sandbox at the versions
// pinned in the GUI, and the forgeai operator at the Helm chart version pinned
// in the GUI. Shown to cluster administrators only.
export function Dependencies() {
  const [data, setData] = useState<app.Dependencies | null>(null)
  const [error, setError] = useState('')
  const [failed, setFailed] = useState('')
  // done is the component whose install finished without error; its banner says so until the next install.
  const [done, setDone] = useState('')
  // steps is the latest install step of each component that is installing, as
  // the step key the backend sent.
  const [steps, setSteps] = useState<Record<string, string>>({})
  // cluster is the context the list was read from. The progress listener reads
  // it from a ref, since it is subscribed once.
  const cluster = useRef('')
  const confirm = useConfirm()
  const active = useActive()

  // fail sets the banner. name is the component whose install failed; its
  // banner offers a retry. Without a name the banner is a plain error.
  const fail = (message: string, name = '') => {
    setError(message)
    setFailed(name)
  }

  // refresh does not clear error: the install outcome is refreshed right after
  // it is set, and the banner must still show it.
  const refresh = () => ListDependencies().then(d => {
    cluster.current = d.cluster
    setData(d)
  }).catch(e => {
    fail(errText(e))
    throw e
  })
  useEffect(() => {
    if (!active) return
    // A failed install keeps its banner and retry offer until the next install,
    // so leaving the page and coming back does not lose them.
    if (!failed) fail('')
    setDone('')
    refresh().catch(() => undefined)
  }, [active])

  // Installs run in the background; their steps arrive as events, and the
  // final one refreshes the list with the outcome.
  useEffect(() => EventsOn('deps:progress', (p: Progress) => {
    // An install on another cluster is not shown here; its steps would mix
    // into this list. Its outcome is seen when that cluster is connected.
    if (p.cluster !== cluster.current) return
    if (!p.done) {
      if (p.step) setSteps(s => ({...s, [p.name]: p.step as string}))
      return
    }
    setSteps(({[p.name]: _, ...rest}) => rest)
    if (p.error) fail(`${title(p.name)}: ${errText(p.error)}`, p.name)
    else setDone(p.name)
    refresh().catch(() => undefined)
  }), [])

  const install = (d: app.Dependency) => confirm.ask({
    title: t('deps.confirmTitle', {action: verb(d), name: title(d.name, d.title)}),
    message: <>
      {t(d.name === 'forgeai' ? 'deps.chartConfirmMessage' : 'deps.confirmMessage', {version: d.pinned})}
      {d.name === 'agent-sandbox' && <><br/>{t('deps.restartNote')}</>}
    </>,
    action: verb(d), primary: true,
    run: () => {
      fail('')
      setDone('')
      setSteps(s => ({...s, [d.name]: 'starting'}))
      InstallDependency(d.name).catch(e => {
        setSteps(({[d.name]: _, ...rest}) => rest)
        fail(errText(e), d.name)
      }).then(() => refresh().catch(() => undefined))
    },
  })

  if (data && !data.clusterAdmin) {
    return (
      <div className="page">
        <Empty icon="shield" title={t('deps.adminsOnly')}>
          {t('deps.adminsOnlyHint')}
        </Empty>
      </div>
    )
  }

  const items = data?.items ?? []
  const ready = items.filter(d => d.status === 'ready').length
  const retry = failed ? items.find(d => d.name === failed) : undefined

  return (
    <div className="page">
      <PageHeader eyebrow={t('nav.admin')} title={t('deps.title')}
                  meta={data && <span className="muted" style={{fontSize: 12.5}}>
                    {items.length === 1 ? t('deps.metaOne', {ready}) : t('deps.meta', {total: items.length, ready})}
                  </span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {done && !error && <div className="banner banner-success" role="status">{t('deps.doneOK', {name: title(done)})}</div>}
      {error && <div className="banner banner-error" role="alert">
        <span>{error}</span>
        {retry && offersInstall(retry) && !steps[retry.name] && <>
          <span className="spacer"/>
          <button className="btn btn-sm btn-primary" onClick={() => install(retry)}>{t('common.retry')}</button>
        </>}
      </div>}
      {confirm.dialog}

      <Card>
        {!data ? (
          error ? null : <Spinner label={t('deps.checking')}/>
        ) : (
          <table className="table table-wrap">
            <thead>
            <tr><th>{t('deps.component')}</th><th>{t('deps.installed')}</th><th>{t('deps.pinned')}</th>
              <th>{t('deps.status')}</th><th/></tr>
            </thead>
            <tbody>
            {items.map(d => {
              const step = steps[d.name]
              const installing = d.status === 'installing'
              const note = installing ? '' : detail(d)
              return (
                <Fragment key={d.name}>
                <tr>
                  <td>
                    <span className="cell-main">{title(d.name, d.title)}</span>
                    <span className="cell-sub">{desc(d.name, d.description)}</span>
                  </td>
                  <td className="mono">
                    {d.installed || (d.running
                      ? <>
                          {/* Each tag stays on one line; the list wraps only between tags. */}
                          {d.running.split(', ').map((tag, i) => (
                            <Fragment key={i}>
                              {i > 0 && ', '}
                              <span style={{whiteSpace: 'nowrap'}}>{tag}</span>
                            </Fragment>
                          ))}
                          <div className="muted" style={{fontSize: 12, marginTop: 4}}>{t('deps.runningHint')}</div>
                        </>
                      : <span className="muted">—</span>)}
                    {d.namespace && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {d.namespace}
                    </div>}
                    {d.appliedBy && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {t('deps.appliedBy', {user: d.appliedBy, time: new Date(d.appliedAt).toLocaleString(getLocale())})}
                    </div>}
                  </td>
                  <td className="mono">{d.pinned || <span className="muted">—</span>}</td>
                  <td>
                    <Badge tone={tone(d.status)} dot>{label(d.status)}</Badge>
                    {installing && !step && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {t('deps.step.starting')}
                    </div>}
                    {note && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {note}
                    </div>}
                  </td>
                  <td>
                    <div className="actions">
                      {offersInstall(d) && !step && (
                        <button className="btn btn-sm btn-primary" onClick={() => install(d)}>
                          <Icon name="download" size={13}/>{verb(d)}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
                {step && <tr className="install-row"><td colSpan={5}><InstallSteps name={d.name} step={step}/></td></tr>}
                </Fragment>
              )
            })}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  )
}

// installSteps is the order of the steps an install emits (app/deps.go).
function installSteps(name: string): string[] {
  if (name === 'forgeai') return ['chart-namespace', 'chart-pull', 'chart-install', 'record']
  const steps = ['namespaces', 'crds', 'controllers', 'deployments', 'webhooks', 'record']
  return name === 'agent-sandbox' ? [...steps, 'restart'] : steps
}

// InstallSteps lists an install's steps in order under its row: the done ones,
// the one running and the ones to come. The bar counts the step running.
function InstallSteps({name, step}: {name: string, step: string}) {
  const steps = installSteps(name)
  // Before the first step is reported the install is only starting: no step is current yet,
  // so the view must not number the first step as running.
  if (!steps.includes(step)) return <div className="install-steps muted">{t('deps.step.starting')}</div>
  const current = steps.indexOf(step)
  return (
    <div className="install-steps">
      <Meter value={current + 1} max={steps.length}
             label={t('deps.stepCount', {current: current + 1, total: steps.length})}/>
      <ol>
        {steps.map((s, i) => (
          <li key={s} className={i < current ? 'done' : i === current ? 'current' : undefined}
              aria-current={i === current ? 'step' : undefined}>
            {i < current ? <Icon name="check" size={12}/> : <span className="step-mark" aria-hidden/>}
            {t(`deps.step.${s}` as MessageKey)}
            {i < current && <span className="visually-hidden">{t('deps.stepDone')}</span>}
          </li>
        ))}
      </ol>
    </div>
  )
}

// title and desc are the translated text of a component, keyed by its name in
// the Go catalog. A component the UI has no copy for shows the backend text
// rather than its message key.
function copy(key: string, fallback: string): string {
  const text = t(key as MessageKey)
  return text === key ? fallback : text
}

function title(name: string, fallback = name): string {
  return copy(`deps.${name}.title`, fallback)
}

function desc(name: string, fallback = ''): string {
  return copy(`deps.${name}.desc`, fallback)
}

// detail is the line under a component's status. The view words the cases it
// has a reason for, and the diagnostics the backend keys; any other message is
// shown as the backend wrote it.
function detail(d: app.Dependency): string {
  if (d.status === 'missing') return '' // the status label already says it
  if (d.status === 'outdated') return t('deps.outdated', {installed: d.installed, pinned: d.pinned})
  switch (d.reason) {
    case 'unmanaged': return t('deps.reason.unmanaged')
    case 'unmanagedHelm': return t('deps.reason.unmanagedHelm')
    case 'namespaceMismatch': return t('deps.reason.namespaceMismatch', {namespace: d.namespace, expected: d.expectedNamespace ?? ''})
    case 'operatorMissing': return t('deps.reason.operatorMissing')
  }
  if (d.messageKey) {
    const key = `deps.msg.${d.messageKey}`
    const text = t(key as MessageKey, d.messageArgs)
    if (text !== key) return text
  }
  return d.message
}

// offersInstall reports whether a component can be installed or upgraded from
// its status. The row and the failure banner both use it.
function offersInstall(d: app.Dependency): boolean {
  return d.installable && d.status !== 'ready' && d.status !== 'installing' && d.status !== 'mismatch'
    && d.reason !== 'unmanagedHelm'
}

// verb is the action a component offers, from its status.
function verb(d: app.Dependency): string {
  switch (d.status) {
    case 'outdated': return t('deps.action.upgrade')
    case 'degraded': return t('deps.action.reinstall')
    default: return t('deps.action.install')
  }
}

function label(status: string): string {
  return t(`deps.status.${status}` as MessageKey)
}

function tone(status: string): 'success' | 'warning' | 'error' | 'accent' | 'neutral' {
  switch (status) {
    case 'ready': return 'success'
    case 'outdated':
    case 'mismatch':
    case 'unmanaged': return 'warning'
    case 'degraded': return 'error'
    case 'installing': return 'accent'
    default: return 'neutral'
  }
}

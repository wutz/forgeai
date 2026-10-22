import {useEffect, useRef, useState} from 'react'
import {InstallDependency, ListDependencies} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Spinner, useActive, useConfirm} from './ui'
import {MessageKey, t} from './i18n'

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
  // steps is the latest install step of each component that is installing, as
  // the step key the backend sent.
  const [steps, setSteps] = useState<Record<string, string>>({})
  // cluster is the context the list was read from. The progress listener reads
  // it from a ref, since it is subscribed once.
  const cluster = useRef('')
  const confirm = useConfirm()
  const active = useActive()

  // refresh does not clear error: the install outcome is refreshed right after
  // it is set, and the banner must still show it.
  const refresh = () => ListDependencies().then(d => {
    cluster.current = d.cluster
    setData(d)
  }).catch(e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => {
    if (!active) return
    setError('')
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
    if (p.error) setError(`${title(p.name)}: ${p.error}`)
    refresh().catch(() => undefined)
  }), [])

  const install = (d: app.Dependency) => confirm.ask({
    title: t('deps.confirmTitle', {action: verb(d), name: title(d.name)}),
    message: <>
      {t(d.name === 'forgeai' ? 'deps.chartConfirmMessage' : 'deps.confirmMessage', {version: d.pinned})}
      {d.name === 'agent-sandbox' && ` ${t('deps.restartNote')}`}
    </>,
    action: verb(d),
    run: () => {
      setError('')
      setSteps(s => ({...s, [d.name]: 'starting'}))
      InstallDependency(d.name).catch(e => {
        setSteps(({[d.name]: _, ...rest}) => rest)
        setError(errText(e))
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

  return (
    <div className="page">
      <PageHeader eyebrow={t('nav.admin')} title={t('deps.title')}
                  meta={data && <span className="muted" style={{fontSize: 12.5}}>
                    {t('deps.meta', {total: items.length, ready})}
                  </span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      <Card>
        {!data ? (
          error ? null : <Spinner label={t('deps.checking')}/>
        ) : (
          <table className="table">
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
                <tr key={d.name}>
                  <td>
                    <span className="cell-main">{title(d.name)}</span>
                    <span className="cell-sub">{desc(d.name)}</span>
                  </td>
                  <td className="mono">
                    {d.installed || <span className="muted">—</span>}
                    {d.namespace && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {d.namespace}
                    </div>}
                    {d.appliedBy && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {t('deps.appliedBy', {user: d.appliedBy, time: new Date(d.appliedAt).toLocaleString()})}
                    </div>}
                  </td>
                  <td className="mono">{d.pinned || <span className="muted">—</span>}</td>
                  <td>
                    <Badge tone={tone(d.status)}>{label(d.status)}</Badge>
                    {(installing || step) && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {t(`deps.step.${step ?? 'starting'}` as MessageKey)}
                    </div>}
                    {note && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {note}
                    </div>}
                  </td>
                  <td>
                    <div className="actions">
                      {d.installable && d.status !== 'ready' && d.status !== 'installing' && d.status !== 'mismatch'
                        && d.reason !== 'unmanagedHelm' && !step && (
                        <button className="btn btn-sm btn-accent" onClick={() => install(d)}>
                          <Icon name="download" size={13}/>{verb(d)}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              )
            })}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  )
}

// title and desc are the translated text of a component, keyed by its name in
// the Go catalog.
function title(name: string): string {
  return t(`deps.${name}.title` as MessageKey)
}

function desc(name: string): string {
  return t(`deps.${name}.desc` as MessageKey)
}

// detail is the line under a component's status. The view words the cases it
// has a reason for; the other messages are backend diagnostics, shown as written.
function detail(d: app.Dependency): string {
  if (d.status === 'missing') return '' // the status label already says it
  if (d.status === 'outdated') return t('deps.outdated', {installed: d.installed, pinned: d.pinned})
  switch (d.reason) {
    case 'unmanaged': return t('deps.reason.unmanaged')
    case 'unmanagedHelm': return t('deps.reason.unmanagedHelm')
    case 'namespaceMismatch': return t('deps.reason.namespaceMismatch', {namespace: d.namespace, expected: d.expectedNamespace ?? ''})
  }
  return d.message
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

import {useEffect, useState} from 'react'
import {InstallDependency, ListDependencies} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Spinner, useActive, useConfirm} from './ui'
import {MessageKey, t} from './i18n'

// Progress is one deps:progress event: a step of an install, or its outcome
// when done is set (app.DependencyProgress). Wails only generates models for
// bound methods, so the event payload is typed here.
type Progress = {name: string, message: string, done: boolean, error?: string}

// Dependencies installs and checks the cluster components forgeai runs on:
// Kubeflow Trainer v2 with JobSet, Kueue and agent-sandbox at the versions
// pinned in the GUI, and the forgeai operator, which is only reported on.
// Shown to cluster administrators only.
export function Dependencies() {
  const [data, setData] = useState<app.Dependencies | null>(null)
  const [error, setError] = useState('')
  // steps is the latest progress message of each component that is installing.
  const [steps, setSteps] = useState<Record<string, string>>({})
  const confirm = useConfirm()
  const active = useActive()

  const refresh = () => ListDependencies().then(d => {
    setData(d); setError('')
  }).catch(e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => { if (active) refresh().catch(() => undefined) }, [active])

  // Installs run in the background; their steps arrive as events, and the
  // final one refreshes the list with the outcome.
  useEffect(() => EventsOn('deps:progress', (p: Progress) => {
    if (!p.done) {
      setSteps(s => ({...s, [p.name]: p.message}))
      return
    }
    setSteps(({[p.name]: _, ...rest}) => rest)
    if (p.error) setError(`${p.name}: ${p.error}`)
    refresh().catch(() => undefined)
  }), [])

  const install = (d: app.Dependency) => confirm.ask({
    title: t('deps.confirmTitle', {action: verb(d), name: text(d, 'title')}),
    message: <>
      {t('deps.confirmMessage', {version: d.pinned})}
      {d.name === 'agent-sandbox' && ` ${t('deps.restartNote')}`}
    </>,
    action: verb(d),
    run: () => {
      setSteps(s => ({...s, [d.name]: t('deps.starting')}))
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
              return (
                <tr key={d.name}>
                  <td>
                    <span className="cell-main">{text(d, 'title')}</span>
                    <span className="cell-sub">{text(d, 'desc')}</span>
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
                      {step || t('deps.installing')}
                    </div>}
                    {!installing && d.message && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {d.message}
                    </div>}
                  </td>
                  <td>
                    <div className="actions">
                      {d.installable && d.status !== 'ready' && d.status !== 'installing' && d.status !== 'mismatch' && (
                        <button className="btn btn-sm btn-primary" onClick={() => install(d)}>
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

// text is the translated title or description of a component; the names are
// the ones in the Go catalog.
function text(d: app.Dependency, part: 'title' | 'desc'): string {
  return t(`deps.${d.name}.${part}` as MessageKey)
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

import {useEffect, useState} from 'react'
import {InstallDependency, ListDependencies} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Spinner, useConfirm} from './ui'

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

  const refresh = () => ListDependencies().then(d => {
    setData(d); setError('')
  }).catch(e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => { refresh().catch(() => undefined) }, [])

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
    title: `${verb(d)} ${d.title}?`,
    message: <>
      Applies {d.pinned} to the connected cluster with server-side apply: objects with the same
      names are overwritten, and nothing is removed.
      {d.name === 'agent-sandbox' && ' The forgeai operator restarts afterwards to start the Sandbox controller.'}
    </>,
    action: verb(d),
    run: () => {
      setSteps(s => ({...s, [d.name]: 'Starting'}))
      InstallDependency(d.name).catch(e => {
        setSteps(({[d.name]: _, ...rest}) => rest)
        setError(errText(e))
      }).then(() => refresh().catch(() => undefined))
    },
  })

  if (data && !data.clusterAdmin) {
    return (
      <div className="page">
        <Empty icon="shield" title="Cluster admins only">
          Installing cluster components needs cluster-admin on this cluster.
        </Empty>
      </div>
    )
  }

  const items = data?.items ?? []
  const ready = items.filter(d => d.status === 'ready').length

  return (
    <div className="page">
      <PageHeader eyebrow="Admin" title="Dependencies"
                  meta={data && <span className="muted" style={{fontSize: 12.5}}>
                    {items.length} components · {ready} ready
                  </span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      <Card>
        {!data ? (
          error ? null : <Spinner label="Checking the cluster…"/>
        ) : (
          <table className="table">
            <thead>
            <tr><th>Component</th><th>Installed</th><th>Pinned</th><th>Status</th><th/></tr>
            </thead>
            <tbody>
            {items.map(d => {
              const step = steps[d.name]
              const installing = d.status === 'installing'
              return (
                <tr key={d.name}>
                  <td>
                    <span className="cell-main">{d.title}</span>
                    <span className="cell-sub">{d.description}</span>
                  </td>
                  <td className="mono">{d.installed || <span className="muted">—</span>}</td>
                  <td className="mono">{d.pinned || <span className="muted">—</span>}</td>
                  <td>
                    <Badge tone={tone(d.status)}>{label(d.status)}</Badge>
                    {(installing || step) && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {step || 'Installing'}
                    </div>}
                    {!installing && d.message && <div className="muted" style={{fontSize: 12, marginTop: 4}}>
                      {d.message}
                    </div>}
                  </td>
                  <td>
                    <div className="actions">
                      {d.installable && d.status !== 'ready' && d.status !== 'installing' && (
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

// verb is the action a component offers, from its status.
function verb(d: app.Dependency): string {
  switch (d.status) {
    case 'outdated': return 'Upgrade'
    case 'degraded': return 'Reinstall'
    default: return 'Install'
  }
}

function label(status: string): string {
  return ({
    ready: 'Ready', missing: 'Not installed', degraded: 'Degraded', unmanaged: 'Unmanaged',
    outdated: 'Outdated', installing: 'Installing',
  } as Record<string, string>)[status] ?? status
}

function tone(status: string): 'success' | 'warning' | 'error' | 'accent' | 'neutral' {
  switch (status) {
    case 'ready': return 'success'
    case 'outdated':
    case 'unmanaged': return 'warning'
    case 'degraded': return 'error'
    case 'installing': return 'accent'
    default: return 'neutral'
  }
}

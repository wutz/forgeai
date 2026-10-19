import {useEffect, useState} from 'react'
import {ListInstanceTypes, ListJobs, ListVolumes, ListImages, Quota} from '../wailsjs/go/app/App'
import {app, volumes} from '../wailsjs/go/models'
import {Card, Icon, MetricTile, Meter, PageHeader, Spinner} from './ui'
import {Status, errText, fmtDate, pct, toGiB} from './util'

// Overview is the landing view of a project: what is running now, the GPU
// catalogue and the quota it may still use. Every figure is derived from the
// same list calls the other views use — there is no separate metrics API.
export function Overview({onOpen}: {onOpen: (area: 'jobs' | 'volumes' | 'resources' | 'images') => void}) {
  const [jobs, setJobs] = useState<app.Job[]>([])
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [vols, setVols] = useState<volumes.Volume[]>([])
  const [images, setImages] = useState<app.ImageRepo[]>([])
  const [quota, setQuota] = useState<app.QuotaUsage | null>(null)
  const [error, setError] = useState('')
  const [loaded, setLoaded] = useState(false)

  const refresh = () => {
    Promise.allSettled([
      ListJobs().then(setJobs),
      ListInstanceTypes().then(setTypes),
      ListVolumes(false).then(setVols),
      ListImages().then(setImages),
      Quota().then(setQuota),
    ]).then(results => {
      const failed = results.find(r => r.status === 'rejected') as PromiseRejectedResult | undefined
      setError(failed ? errText(failed.reason) : '')
      setLoaded(true)
    })
  }
  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 8000)
    return () => clearInterval(t)
  }, [])

  const running = jobs.filter(j => j.phase === 'Running')
  const queued = jobs.filter(j => j.phase === 'Queued' || j.phase === 'Pending')
  const failed = jobs.filter(j => j.phase === 'Failed')
  const readyWorkers = jobs.reduce((n, j) => n + (j.ready || 0), 0)
  const wantedWorkers = jobs.reduce((n, j) => n + (j.workers || 0), 0)

  const gpuTotal = types.reduce((n, t) => n + t.total, 0)
  const gpuFree = types.reduce((n, t) => n + t.free, 0)
  const gpuUsed = Math.max(0, gpuTotal - gpuFree)

  const storageGiB = vols.reduce((n, v) => n + (v.capacityGiB || v.requestedGiB || 0), 0)
  const totalTags = images.reduce((n, r) => n + r.count, 0)

  if (!loaded) return <div className="page"><Spinner label="Loading overview…"/></div>

  return (
    <div className="page">
      <PageHeader eyebrow="Workspace" title="Overview"
                  meta={<span className="muted" style={{fontSize: 12.5}}>live · every 8s</span>}
                  actions={<button className="btn btn-sm btn-ghost" onClick={refresh}>
                    <Icon name="refresh" size={14}/>Refresh
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}

      <div className="metrics">
        <MetricTile icon="play" label="Running jobs" tone="success"
                    value={running.length} unit={`/ ${jobs.length}`}
                    detail={`${readyWorkers} of ${wantedWorkers} workers ready`}>
          <Meter value={running.length} max={Math.max(jobs.length, 1)} tone="success"/>
        </MetricTile>
        <MetricTile icon="clock" label="Queued" tone="warning"
                    value={queued.length}
                    detail={queued.length ? 'Waiting for GPU quota' : 'Nothing waiting'}>
          <Meter value={queued.length} max={Math.max(jobs.length, 1)} tone="warning"/>
        </MetricTile>
        <MetricTile icon="warning" label="Failed" tone={failed.length ? 'error' : undefined}
                    value={failed.length}
                    detail={failed.length ? 'Open Jobs to inspect' : 'No failures'}>
          <Meter value={failed.length} max={Math.max(jobs.length, 1)} tone={failed.length ? 'error' : 'muted'}/>
        </MetricTile>
        <MetricTile icon="cpu" label="GPUs in use" tone="ember"
                    value={gpuUsed} unit={gpuTotal ? `/ ${gpuTotal}` : undefined}
                    detail={gpuTotal ? `${gpuFree} free in the cluster` : 'No GPU nodes detected'}>
          <Meter value={gpuUsed} max={Math.max(gpuTotal, 1)} tone="ember"/>
        </MetricTile>
      </div>

      <div className="overview-grid">
        <Card title="GPU pools" subtitle="Instance types the cluster offers"
              actions={<button className="btn btn-sm btn-ghost" onClick={() => onOpen('resources')}>
                Resources<Icon name="chevron" size={13}/>
              </button>}>
          {types.length === 0 ? (
            <div className="empty"><div className="empty-title">No GPU nodes detected</div></div>
          ) : (
            <div className="pool-list">
              {types.map(t => (
                <div className="pool" key={t.name}>
                  <div className="pool-head">
                    <span className="mono pool-name">{t.name}</span>
                    <span className="badge">{t.gpuCount}× {t.product}</span>
                    <span className="spacer"/>
                    <span className="pool-free">
                      <strong className={t.free >= 1 ? 'free' : 'muted'}>{t.free}</strong>
                      <span className="muted"> / {t.total} free</span>
                    </span>
                  </div>
                  <Meter value={t.total - t.free} max={Math.max(t.total, 1)}
                         tone={t.free >= 1 ? 'success' : 'muted'}
                         label={<span>{t.cpu} · {t.memory} per worker</span>}
                         right={<span>{pct(t.total - t.free, t.total)}% used</span>}/>
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card title="Quota" subtitle="What this project may still use">
          {!quota ? <div className="empty"><div className="empty-title">Quota unavailable</div></div> : (
            <div className="quota-list">
              {(quota.gpuClasses || []).map(g => (
                <Meter key={g.class} value={g.used} max={Math.max(g.quota, 1)}
                       tone={g.used >= g.quota ? 'warning' : 'ember'}
                       label={<span className="mono">GPUs · {g.class}</span>}
                       right={<span>{g.used} / {g.quota}</span>}/>
              ))}
              <Meter value={toGiB(quota.storageUsed)} max={Math.max(toGiB(quota.storage), 1)} tone="muted"
                     label={<span className="mono">Volume storage</span>}
                     right={<span>{quota.storageUsed || '0'} / {quota.storage || 'unlimited'}</span>}/>
              <Meter value={quota.volumes} max={Math.max(quota.volumeMax, quota.volumes, 1)} tone="muted"
                     label={<span className="mono">Volumes</span>}
                     right={<span>{quota.volumes} / {quota.volumeMax || 'unlimited'}</span>}/>
              <Meter value={quota.pods} max={Math.max(quota.podMax, quota.pods, 1)} tone="muted"
                     label={<span className="mono">Pods</span>}
                     right={<span>{quota.pods} / {quota.podMax || 'unlimited'}</span>}/>
            </div>
          )}
        </Card>
      </div>

      <div className="overview-grid overview-grid-even">
        <Card title="Recent jobs" subtitle="Newest first"
              actions={<button className="btn btn-sm btn-ghost" onClick={() => onOpen('jobs')}>
                All jobs<Icon name="chevron" size={13}/>
              </button>}>
          {jobs.length === 0 ? (
            <div className="empty">
              <div className="empty-title">No jobs yet</div>
              <div className="empty-text">Create one from Jobs to start training.</div>
            </div>
          ) : (
            <table className="table">
              <thead><tr><th>Job</th><th>Status</th><th>Ready</th><th>Created</th></tr></thead>
              <tbody>
              {jobs.slice(0, 6).map(j => (
                <tr key={j.name} onClick={() => onOpen('jobs')}>
                  <td className="mono trunc" title={j.name}>{j.name}</td>
                  <td><Status phase={j.phase}/></td>
                  <td className="muted">{j.ready}/{j.workers}</td>
                  <td className="muted">{fmtDate(j.created)}</td>
                </tr>
              ))}
              </tbody>
            </table>
          )}
        </Card>

        <Card title="Storage" subtitle="Volumes and the project registry">
          <div className="stat-row">
            <div className="stat">
              <div className="stat-label">Volumes</div>
              <div className="stat-value">{vols.length}</div>
              <div className="stat-detail">{storageGiB ? `${storageGiB} GiB allocated` : 'none yet'}</div>
              <button className="btn btn-sm btn-ghost" onClick={() => onOpen('volumes')}>
                Manage<Icon name="chevron" size={13}/>
              </button>
            </div>
            <div className="stat">
              <div className="stat-label">Storage quota</div>
              <div className="stat-value">{quota?.storage || '∞'}</div>
              <div className="stat-detail">{quota?.storageUsed ? `${quota.storageUsed} used` : 'nothing used'}</div>
            </div>
            <div className="stat">
              <div className="stat-label">Image tags</div>
              <div className="stat-value">{totalTags}</div>
              <div className="stat-detail">{images.length} repositories</div>
              <button className="btn btn-sm btn-ghost" onClick={() => onOpen('images')}>
                Images<Icon name="chevron" size={13}/>
              </button>
            </div>
          </div>
        </Card>
      </div>
    </div>
  )
}

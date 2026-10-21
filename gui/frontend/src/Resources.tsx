import {useEffect, useState} from 'react'
import {ListInstanceTypes, Quota} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, pct, toGiB} from './util'
import {Badge, Card, Empty, Icon, Meter, MetricTile, PageHeader, RefreshButton} from './ui'

// Resources shows the cluster GPU catalogue (instance types with free
// counts) and the project's quota and its use.
export function Resources() {
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [quota, setQuota] = useState<app.QuotaUsage | null>(null)
  const [error, setError] = useState('')

  // load rejects when either list failed; the banner already shows why.
  const load = () => Promise.all([
    ListInstanceTypes().then(setTypes),
    Quota().then(setQuota),
  ]).then(() => setError(''), e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => {
    const poll = () => load().catch(() => {})
    poll()
    const t = setInterval(poll, 10000)
    return () => clearInterval(t)
  }, [])

  const total = types.reduce((n, t) => n + t.total, 0)
  const free = types.reduce((n, t) => n + t.free, 0)
  const used = Math.max(0, total - free)
  const gpuQuota = (quota?.gpuClasses || []).reduce((n, g) => n + g.quota, 0)
  const gpuUsed = (quota?.gpuClasses || []).reduce((n, g) => n + g.used, 0)

  return (
    <div className="page">
      <PageHeader eyebrow="Cluster" title="Resources"
                  meta={<span className="muted" style={{fontSize: 12.5}}>live · every 10s</span>}
                  actions={<RefreshButton onRefresh={load}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}

      <div className="metrics">
        <MetricTile icon="cpu" label="GPUs in the cluster" tone="accent"
                    value={total} detail={`${types.length} instance types`}/>
        <MetricTile icon="check" label="Free now" tone="success"
                    value={free} detail={total ? `${pct(used, total)}% in use` : 'no GPU nodes'}/>
        <MetricTile icon="shield" label="Project GPU quota" tone="violet"
                    value={gpuQuota || '—'} detail={gpuQuota ? `${gpuUsed} used by this project` : 'no quota set'}/>
        <MetricTile icon="database" label="Storage quota" tone="warning"
                    value={quota?.storage || '∞'} detail={quota?.storageUsed ? `${quota.storageUsed} used` : 'nothing used'}/>
      </div>

      <Card title="Instance types" subtitle="GPU classes the scheduler can place workers on">
        {types.length === 0 ? (
          <Empty icon="cpu" title="No GPU nodes detected">
            Install the GPU operator or add nodes labelled with a forgeai instance type.
          </Empty>
        ) : (
          <table className="table">
            <thead>
            <tr><th>Type</th><th>GPU</th><th>CPU / worker</th><th>Memory / worker</th>
              <th>Capacity</th><th style={{width: 200}}>Free</th></tr>
            </thead>
            <tbody>
            {types.map(t => (
              <tr key={t.name} className={t.free >= 1 ? '' : 'row-full'}>
                <td className="mono">
                  <span className="cell-main">{t.name}</span>
                  <span className="cell-sub">{t.class}</span>
                </td>
                <td><Badge>{t.gpuCount}× {t.product}</Badge></td>
                <td>{t.cpu}</td>
                <td>{t.memory}</td>
                <td className="mono">{t.total}</td>
                <td>
                  <Meter value={t.total - t.free} max={Math.max(t.total, 1)}
                         tone={t.free >= 1 ? 'success' : 'muted'}
                         label={<span><strong className={t.free >= 1 ? 'free' : 'muted'}>{t.free}</strong> free</span>}
                         right={<span>{pct(t.total - t.free, t.total)}%</span>}/>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </Card>

      {quota && (
        <Card title="Project quota" subtitle="Kueue queue limit and its backstop ResourceQuota"
              className="card-spaced">
          <table className="table">
            <thead><tr><th>Resource</th><th style={{width: 260}}>Use</th><th>Used</th><th>Limit</th></tr></thead>
            <tbody>
            {quota.gpuClasses?.map(g => (
              <tr key={g.class}>
                <td className="mono">GPUs ({g.class})</td>
                <td>
                  <Meter value={g.used} max={Math.max(g.quota, 1)}
                         tone={g.used >= g.quota ? 'warning' : 'accent'}
                         right={<span>{pct(g.used, g.quota)}%</span>}/>
                </td>
                <td>{g.used}</td>
                <td>{g.quota}</td>
              </tr>
            ))}
            <tr>
              <td className="mono">Volume storage</td>
              <td>
                <Meter value={toGiB(quota.storageUsed)} max={Math.max(toGiB(quota.storage), 1)} tone="muted"
                       right={<span>{pct(toGiB(quota.storageUsed), toGiB(quota.storage))}%</span>}/>
              </td>
              <td>{quota.storageUsed || '0'}</td>
              <td>{quota.storage || 'unlimited'}</td>
            </tr>
            <tr>
              <td className="mono">Volumes</td>
              <td><Meter value={quota.volumes} max={Math.max(quota.volumeMax, quota.volumes, 1)} tone="muted"/></td>
              <td>{quota.volumes}</td>
              <td>{quota.volumeMax || '—'}</td>
            </tr>
            <tr>
              <td className="mono">Pods</td>
              <td><Meter value={quota.pods} max={Math.max(quota.podMax, quota.pods, 1)} tone="muted"/></td>
              <td>{quota.pods}</td>
              <td>{quota.podMax || '—'}</td>
            </tr>
            </tbody>
          </table>
        </Card>
      )}
    </div>
  )
}

import {useEffect, useState} from 'react'
import {ListInstanceTypes, Quota} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, pct, toGiB} from './util'
import {Badge, Card, Empty, Icon, Meter, MetricTile, PageHeader, RefreshButton, useActive} from './ui'
import {t} from './i18n'

// Resources shows the cluster GPU catalogue (instance types with free
// counts) and the project's quota and its use.
export function Resources() {
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [quota, setQuota] = useState<app.QuotaUsage | null>(null)
  const [error, setError] = useState('')
  const active = useActive()

  // load rejects when either list failed; the banner already shows why.
  const load = () => Promise.all([
    ListInstanceTypes().then(setTypes),
    Quota().then(setQuota),
  ]).then(() => setError(''), e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => {
    if (!active) return
    const poll = () => load().catch(() => {})
    poll()
    const timer = setInterval(poll, 10000)
    return () => clearInterval(timer)
  }, [active])

  const total = types.reduce((n, ty) => n + ty.total, 0)
  const free = types.reduce((n, ty) => n + ty.free, 0)
  const used = Math.max(0, total - free)
  const gpuQuota = (quota?.gpuClasses || []).reduce((n, g) => n + g.quota, 0)
  const gpuUsed = (quota?.gpuClasses || []).reduce((n, g) => n + g.used, 0)

  return (
    <div className="page">
      <PageHeader eyebrow={t('common.cluster')} title={t('resources.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{t('resources.live')}</span>}
                  actions={<RefreshButton onRefresh={load}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}

      <div className="metrics">
        <MetricTile icon="cpu" label={t('resources.gpusInCluster')} tone="accent"
                    value={total} detail={t('resources.instanceTypeCount', {count: types.length})}/>
        <MetricTile icon="check" label={t('resources.freeNow')} tone="success"
                    value={free} detail={total ? t('resources.percentInUse', {percent: pct(used, total)}) : t('resources.noGpuNodes')}/>
        <MetricTile icon="shield" label={t('resources.projectGpuQuota')} tone="violet"
                    value={gpuQuota || '—'} detail={gpuQuota ? t('resources.usedByProject', {used: gpuUsed}) : t('resources.noQuotaSet')}/>
        <MetricTile icon="database" label={t('resources.storageQuota')} tone="warning"
                    value={quota?.storage || '∞'} detail={quota?.storageUsed ? t('resources.storageUsed', {used: quota.storageUsed}) : t('resources.nothingUsed')}/>
      </div>

      <Card title={t('resources.instanceTypes')} subtitle={t('resources.instanceTypesSubtitle')}>
        {types.length === 0 ? (
          <Empty icon="cpu" title={t('resources.noGpuNodesTitle')}>
            {t('resources.noGpuNodesHint')}
          </Empty>
        ) : (
          <table className="table">
            <thead>
            <tr><th>{t('resources.colType')}</th><th>{t('resources.colGpu')}</th><th>{t('resources.colCpuPerWorker')}</th><th>{t('resources.colMemoryPerWorker')}</th>
              <th>{t('resources.colCapacity')}</th><th style={{width: 200}}>{t('resources.colFree')}</th></tr>
            </thead>
            <tbody>
            {types.map(ty => (
              <tr key={ty.name} className={ty.free >= 1 ? '' : 'row-full'}>
                <td className="mono">
                  <span className="cell-main">{ty.name}</span>
                  <span className="cell-sub">{ty.class}</span>
                </td>
                <td><Badge>{ty.gpuCount}× {ty.product}</Badge></td>
                <td>{ty.cpu}</td>
                <td>{ty.memory}</td>
                <td className="mono">{ty.total}</td>
                <td>
                  <Meter value={ty.total - ty.free} max={Math.max(ty.total, 1)}
                         tone={ty.free >= 1 ? 'success' : 'muted'}
                         label={<span><strong className={ty.free >= 1 ? 'free' : 'muted'}>{ty.free}</strong> {t('resources.free')}</span>}
                         right={<span>{pct(ty.total - ty.free, ty.total)}%</span>}/>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </Card>

      {quota && (
        <Card title={t('resources.projectQuota')} subtitle={t('resources.projectQuotaSubtitle')}
              className="card-spaced">
          <table className="table">
            <thead><tr><th>{t('resources.colResource')}</th><th style={{width: 260}}>{t('resources.colUse')}</th><th>{t('resources.colUsed')}</th><th>{t('resources.colLimit')}</th></tr></thead>
            <tbody>
            {quota.gpuClasses?.map(g => (
              <tr key={g.class}>
                <td className="mono">{t('resources.gpusOfClass', {cls: g.class})}</td>
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
              <td className="mono">{t('resources.volumeStorage')}</td>
              <td>
                <Meter value={toGiB(quota.storageUsed)} max={Math.max(toGiB(quota.storage), 1)} tone="muted"
                       right={<span>{pct(toGiB(quota.storageUsed), toGiB(quota.storage))}%</span>}/>
              </td>
              <td>{quota.storageUsed || '0'}</td>
              <td>{quota.storage || t('resources.unlimited')}</td>
            </tr>
            <tr>
              <td className="mono">{t('resources.volumes')}</td>
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

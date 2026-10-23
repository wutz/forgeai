import {useEffect, useState} from 'react'
import {ListInstanceTypes, Quota} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, everyWhileVisible, pct, toGiB} from './util'
import {Badge, Card, Empty, Icon, Meter, MetricTile, PageHeader, RefreshButton, Spinner, useActive} from './ui'
import {t} from './i18n'

// Resources shows the cluster GPU catalogue (instance types with free
// counts) and the project's quota and its use.
export function Resources() {
  // types is null until the first read, so "no GPU nodes" is not shown while loading.
  const [types, setTypes] = useState<app.InstanceType[] | null>(null)
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
    return everyWhileVisible(poll, 10000)
  }, [active])

  // total and free count instances; the GPU tiles count GPUs, so each GPU type
  // is weighted by its GPUs per instance. CPU-only types have no GPUs to count.
  const list = types ?? []
  const gpuTypes = list.filter(ty => ty.gpuCount > 0)
  const total = gpuTypes.reduce((n, ty) => n + ty.total * ty.gpuCount, 0)
  const free = gpuTypes.reduce((n, ty) => n + ty.free * ty.gpuCount, 0)
  const used = Math.max(0, total - free)
  const gpuQuota = quota?.gpuQuota ?? 0
  const gpuUsed = (quota?.gpuClasses || []).reduce((n, g) => n + g.used, 0)

  return (
    <div className="page">
      <PageHeader eyebrow={t('common.cluster')} title={t('resources.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{t('resources.live')}</span>}
                  actions={<RefreshButton onRefresh={load}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}

      <div className="metrics">
        <MetricTile icon="cpu" label={t('resources.gpusInCluster')} tone="accent"
                    value={types === null ? '—' : total} detail={list.length === 1 ? t('resources.instanceTypeOne') : t('resources.instanceTypeCount', {count: list.length})}/>
        <MetricTile icon="check" label={t('resources.freeNow')} tone="success"
                    value={types === null ? '—' : free} detail={!types ? '' : total ? t('resources.percentInUse', {percent: pct(used, total)}) : t('resources.noGpuNodes')}/>
        <MetricTile icon="shield" label={t('resources.projectGpuQuota')} tone="violet"
                    value={quota === null ? '—' : gpuQuota || '—'}
                    detail={quota === null ? '' : gpuQuota ? t('resources.usedByProject', {used: gpuUsed}) : t('resources.noQuotaSet')}/>
        <MetricTile icon="database" label={t('resources.storageQuota')} tone="warning"
                    value={quota === null ? '—' : quota.storage || '∞'}
                    detail={quota === null ? '' : quota.storageUsed && quota.storageUsed !== '0' ? t('resources.storageUsed', {used: quota.storageUsed}) : t('resources.nothingUsed')}/>
      </div>

      <Card title={t('resources.instanceTypes')} subtitle={t('resources.instanceTypesSubtitle')}>
        {!types ? (
          error ? null : <Spinner label={t('common.loading')}/>
        ) : types.length === 0 ? (
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
                <td className="mono nowrap">
                  <span className="cell-main">{ty.name}</span>
                  <span className="cell-sub">{ty.class}</span>
                </td>
                <td>{ty.gpuCount
                  ? <Badge>{ty.gpuCount}× {ty.product}</Badge>
                  : <span className="muted">{t('resources.cpuOnly')}</span>}</td>
                <td>{ty.cpu}</td>
                <td>{ty.memory}</td>
                <td className="mono">{ty.total}</td>
                <td>
                  <Meter value={ty.total - ty.free} max={Math.max(ty.total, 1)}
                         tone={ty.free >= 1 ? 'success' : 'muted'}
                         label={<span><strong className={ty.free >= 1 ? 'free' : 'muted'}>{ty.free}</strong> {t('resources.free')}</span>}
                         right={<span>{t('resources.percentInUse', {percent: pct(ty.total - ty.free, ty.total)})}</span>}/>
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
            {/* A zero limit means the quota sets none, so there is no bar to fill. */}
            {quota.gpuClasses?.map(g => (
              <tr key={g.class}>
                <td className="mono">{g.class === '*' ? t('resources.gpusUnknownClass') : t('resources.gpusOfClass', {cls: g.class})}</td>
                <td>
                  {quota.gpuQuota ? (
                    <Meter value={g.used} max={Math.max(quota.gpuQuota, g.used, 1)}
                           tone={g.used >= quota.gpuQuota ? 'warning' : 'accent'}
                           right={<span>{t('resources.percentInUse', {percent: pct(g.used, quota.gpuQuota)})}</span>}/>
                  ) : <span className="muted">{t('resources.noQuotaSet')}</span>}
                </td>
                <td>{g.used}</td>
                <td>{quota.gpuQuota || t('resources.noQuotaSet')}</td>
              </tr>
            ))}
            <tr>
              <td className="mono">{t('resources.volumeStorage')}</td>
              <td>
                {quota.storage ? (
                  <Meter value={toGiB(quota.storageUsed)} max={Math.max(toGiB(quota.storage), 1)} tone="muted"
                         right={<span>{t('resources.percentInUse', {percent: pct(toGiB(quota.storageUsed), toGiB(quota.storage))})}</span>}/>
                ) : <span className="muted">{t('resources.unlimited')}</span>}
              </td>
              <td>{quota.storageUsed || '0'}</td>
              <td>{quota.storage || t('resources.unlimited')}</td>
            </tr>
            <tr>
              <td className="mono">{t('resources.volumes')}</td>
              <td>
                {quota.volumeMax ? (
                  <Meter value={quota.volumes} max={Math.max(quota.volumeMax, quota.volumes, 1)} tone="muted"
                         right={<span>{t('resources.percentInUse', {percent: pct(quota.volumes, quota.volumeMax)})}</span>}/>
                ) : <span className="muted">{t('resources.unlimited')}</span>}
              </td>
              <td>{quota.volumes}</td>
              <td>{quota.volumeMax || t('resources.unlimited')}</td>
            </tr>
            <tr>
              <td className="mono">{t('resources.pods')}</td>
              <td>
                {quota.podMax ? (
                  <Meter value={quota.pods} max={Math.max(quota.podMax, quota.pods, 1)} tone="muted"
                         right={<span>{t('resources.percentInUse', {percent: pct(quota.pods, quota.podMax)})}</span>}/>
                ) : <span className="muted">{t('resources.unlimited')}</span>}
              </td>
              <td>{quota.pods}</td>
              <td>{quota.podMax || t('resources.unlimited')}</td>
            </tr>
            </tbody>
          </table>
        </Card>
      )}
    </div>
  )
}

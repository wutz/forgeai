import {useEffect, useState} from 'react'
import {ListInstanceTypes, ListJobs, ListVolumes, ListImages, Quota} from '../wailsjs/go/app/App'
import {app, volumes} from '../wailsjs/go/models'
import {Card, Icon, MetricTile, Meter, PageHeader, RefreshButton, Spinner, useActive} from './ui'
import {Status, errText, fmtDate, pct, phaseLabel, tagCount, toGiB} from './util'
import {t} from './i18n'

// Overview is the landing view of a project: what is running now, the GPU
// catalogue and the quota it may still use. Every figure is derived from the
// same list calls the other views use — there is no separate metrics API.
export function Overview({onOpen}: {onOpen: (area: 'jobs' | 'volumes' | 'resources' | 'images') => void}) {
  // jobs and types are null until their first read, so a failed read is not shown as an empty list.
  const [jobs, setJobs] = useState<app.Job[] | null>(null)
  const [types, setTypes] = useState<app.InstanceType[] | null>(null)
  const [vols, setVols] = useState<volumes.Volume[]>([])
  const [images, setImages] = useState<app.ImageRepo[]>([])
  const [quota, setQuota] = useState<app.QuotaUsage | null>(null)
  const [error, setError] = useState('')
  const [loaded, setLoaded] = useState(false)
  const onScreen = useActive()

  // refresh rejects when any list failed; the banner already shows why.
  const refresh = () => Promise.allSettled([
    ListJobs().then(setJobs),
    ListInstanceTypes().then(setTypes),
    ListVolumes(false).then(setVols),
    Quota().then(setQuota),
  ]).then(results => {
    const failed = results.find(r => r.status === 'rejected') as PromiseRejectedResult | undefined
    setError(failed ? errText(failed.reason) : '')
    setLoaded(true)
    if (failed) throw failed.reason
  })
  useEffect(() => {
    if (!onScreen) return
    // A slow reply must not overlap the next poll: the older answer could land
    // last and show stale numbers.
    let inFlight = false
    const poll = () => {
      if (inFlight) return
      inFlight = true
      refresh().catch(() => {}).finally(() => { inFlight = false })
    }
    poll()
    const timer = setInterval(poll, 8000)
    return () => clearInterval(timer)
  }, [onScreen])

  // Images cost a Harbor call per repository, so they refresh on a slower beat.
  useEffect(() => {
    if (!onScreen) return
    const load = () => ListImages().then(setImages).catch(() => {})
    load()
    const timer = setInterval(load, 60000)
    return () => clearInterval(timer)
  }, [onScreen])

  const jobList = jobs ?? []
  const typeList = types ?? []
  const running = jobList.filter(j => j.phase === 'Running')
  const queued = jobList.filter(j => j.phase === 'Queued' || j.phase === 'Pending')
  const failed = jobList.filter(j => j.phase === 'Failed')
  // Workers count only jobs that still hold or wait for pods: a finished,
  // failed or stopped job no longer expects its workers to be ready.
  const active = jobList.filter(j => j.phase !== 'Succeeded' && j.phase !== 'Failed' && j.phase !== 'Stopped')
  const readyWorkers = active.reduce((n, j) => n + (j.ready || 0), 0)
  const wantedWorkers = active.reduce((n, j) => n + (j.workers || 0), 0)

  // total and free count instances; the GPU tiles count GPUs, so each GPU type
  // is weighted by its GPUs per instance, as on Resources. CPU-only types have none.
  const gpuTypes = typeList.filter(ty => ty.gpuCount > 0)
  const gpuTotal = gpuTypes.reduce((n, ty) => n + ty.total * ty.gpuCount, 0)
  const gpuFree = gpuTypes.reduce((n, ty) => n + ty.free * ty.gpuCount, 0)
  const gpuUsed = Math.max(0, gpuTotal - gpuFree)

  const storageGiB = vols.reduce((n, v) => n + (v.capacityGiB || v.requestedGiB || 0), 0)
  const totalTags = images.reduce((n, r) => n + tagCount(r), 0)

  if (!loaded) return <div className="page"><Spinner label={t('overview.loading')}/></div>

  return (
    <div className="page">
      <PageHeader eyebrow={t('overview.eyebrow')} title={t('overview.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{t('overview.live')}</span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}

      <div className="metrics">
        <MetricTile icon="play" label={t('overview.runningJobs')} tone="success"
                    value={running.length} unit={`/ ${jobList.length}`}
                    detail={t('overview.workersReady', {ready: readyWorkers, wanted: wantedWorkers})}>
          <Meter value={running.length} max={Math.max(jobList.length, 1)} tone="success"/>
        </MetricTile>
        <MetricTile icon="clock" label={phaseLabel('Queued')} tone="warning"
                    value={queued.length}
                    detail={queued.length ? t('overview.waitingForQuota') : t('overview.nothingWaiting')}>
          <Meter value={queued.length} max={Math.max(jobList.length, 1)} tone="warning"/>
        </MetricTile>
        <MetricTile icon="warning" label={phaseLabel('Failed')} tone={failed.length ? 'error' : undefined}
                    value={failed.length}
                    detail={failed.length ? t('overview.openJobs') : t('overview.noFailures')}>
          <Meter value={failed.length} max={Math.max(jobList.length, 1)} tone={failed.length ? 'error' : 'muted'}/>
        </MetricTile>
        <MetricTile icon="cpu" label={t('overview.gpusInUse')} tone="accent"
                    value={gpuUsed} unit={gpuTotal ? `/ ${gpuTotal}` : undefined}
                    detail={gpuTotal ? t('overview.freeInCluster', {free: gpuFree}) : t('overview.noGpuNodes')}>
          <Meter value={gpuUsed} max={Math.max(gpuTotal, 1)} tone="accent"/>
        </MetricTile>
      </div>

      <div className="overview-grid">
        <Card title={t('overview.gpuPools')} subtitle={t('overview.gpuPoolsSubtitle')}
              actions={<button className="btn btn-sm btn-ghost" onClick={() => onOpen('resources')}>
                {t('overview.resourcesButton')}<Icon name="chevron" size={13}/>
              </button>}>
          {types === null ? null : types.length === 0 ? (
            <div className="empty"><div className="empty-title">{t('overview.noGpuNodes')}</div></div>
          ) : (
            <div className="pool-list">
              {types.map(pool => (
                <div className="pool" key={pool.name}>
                  <div className="pool-head">
                    <span className="mono pool-name">{pool.name}</span>
                    {pool.gpuCount
                      ? <span className="badge">{pool.gpuCount}× {pool.product}</span>
                      : <span className="muted">{t('resources.cpuOnly')}</span>}
                    <span className="spacer"/>
                    <span className="pool-free">
                      <strong className={pool.free >= 1 ? 'free' : 'muted'}>{pool.free}</strong>
                      <span className="muted"> {t('overview.freeOfTotal', {total: pool.total})}</span>
                    </span>
                  </div>
                  <Meter value={pool.total - pool.free} max={Math.max(pool.total, 1)}
                         tone={pool.free >= 1 ? 'success' : 'muted'}
                         label={<span>{t('overview.resourcesPerWorker', {cpu: pool.cpu, memory: pool.memory})}</span>}
                         right={<span>{t('overview.percentUsed', {percent: pct(pool.total - pool.free, pool.total)})}</span>}/>
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card title={t('overview.quota')} subtitle={t('overview.quotaSubtitle')}>
          {!quota ? <div className="empty"><div className="empty-title">{t('overview.quotaUnavailable')}</div></div> : (
            <div className="quota-list">
              {(quota.gpuClasses || []).map(g => (
                <Meter key={g.class} value={g.used} max={Math.max(quota.gpuQuota, g.used, 1)}
                       tone={quota.gpuQuota && g.used >= quota.gpuQuota ? 'warning' : 'accent'}
                       label={<span className="mono">{t('overview.gpusOfClass', {cls: g.class})}</span>}
                       right={<span>{quota.gpuQuota ? `${g.used} / ${quota.gpuQuota}` : g.used}</span>}/>
              ))}
              {/* A zero or empty limit means unlimited: the bar stays empty rather than full. */}
              <Meter value={toGiB(quota.storageUsed)} max={toGiB(quota.storage)} tone="muted"
                     label={<span className="mono">{t('overview.volumeStorage')}</span>}
                     right={<span>{quota.storageUsed || '0'} / {quota.storage || t('overview.unlimited')}</span>}/>
              <Meter value={quota.volumes} max={quota.volumeMax} tone="muted"
                     label={<span className="mono">{t('overview.volumes')}</span>}
                     right={<span>{quota.volumes} / {quota.volumeMax || t('overview.unlimited')}</span>}/>
              <Meter value={quota.pods} max={quota.podMax} tone="muted"
                     label={<span className="mono">{t('overview.pods')}</span>}
                     right={<span>{quota.pods} / {quota.podMax || t('overview.unlimited')}</span>}/>
            </div>
          )}
        </Card>
      </div>

      <div className="overview-grid overview-grid-even">
        <Card title={t('overview.recentJobs')} subtitle={t('overview.newestFirst')}
              actions={<button className="btn btn-sm btn-ghost" onClick={() => onOpen('jobs')}>
                {t('overview.allJobs')}<Icon name="chevron" size={13}/>
              </button>}>
          {jobs === null ? null : jobs.length === 0 ? (
            <div className="empty">
              <div className="empty-title">{t('overview.noJobs')}</div>
              <div className="empty-text">{t('overview.createJobHint')}</div>
            </div>
          ) : (
            <table className="table">
              <thead><tr><th>{t('overview.colJob')}</th><th>{t('common.status')}</th><th>{t('overview.colReady')}</th><th>{t('common.created')}</th></tr></thead>
              <tbody>
              {jobs.slice(0, 6).map(j => (
                <tr key={j.name} className="clickable" onClick={() => onOpen('jobs')}>
                  <td className="mono trunc" title={j.name}><button type="button" className="row-name">{j.name}</button></td>
                  <td><Status phase={j.phase}/></td>
                  <td className="muted">{j.ready}/{j.workers}</td>
                  <td className="muted">{fmtDate(j.created)}</td>
                </tr>
              ))}
              </tbody>
            </table>
          )}
        </Card>

        <Card title={t('overview.storage')} subtitle={t('overview.storageSubtitle')}>
          <div className="stat-row">
            <div className="stat">
              <div className="stat-label">{t('overview.volumes')}</div>
              <div className="stat-value">{vols.length}</div>
              <div className="stat-detail">{storageGiB ? t('overview.allocated', {size: storageGiB}) : t('overview.noneYet')}</div>
              <button className="btn btn-sm btn-ghost" onClick={() => onOpen('volumes')}>
                {t('overview.manage')}<Icon name="chevron" size={13}/>
              </button>
            </div>
            <div className="stat">
              <div className="stat-label">{t('overview.storageQuota')}</div>
              <div className="stat-value">{quota?.storage || '∞'}</div>
              <div className="stat-detail">{quota?.storageUsed ? t('overview.storageUsed', {used: quota.storageUsed}) : t('overview.nothingUsed')}</div>
            </div>
            <div className="stat">
              <div className="stat-label">{t('overview.imageTags')}</div>
              <div className="stat-value">{totalTags}</div>
              <div className="stat-detail">{images.length === 1 ? t('overview.repositoryOne') : t('overview.repositories', {count: images.length})}</div>
              <button className="btn btn-sm btn-ghost" onClick={() => onOpen('images')}>
                {t('overview.imagesButton')}<Icon name="chevron" size={13}/>
              </button>
            </div>
          </div>
        </Card>
      </div>
    </div>
  )
}

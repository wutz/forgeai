import {useEffect, useState} from 'react'
import {ListInstanceTypes, ListJobs, ListVolumes, ListImages, Quota} from '../wailsjs/go/app/App'
import {app, volumes} from '../wailsjs/go/models'
import {Card, Icon, MetricTile, Meter, PageHeader, RefreshButton, Spinner, useActive} from './ui'
import {Status, errText, everyWhileVisible, fmtDate, jobBucket, latestAttempt, pct, phaseLabel, tagCount, toGiB} from './util'
import {t} from './i18n'

// Overview is the landing view of a project: what is running now, the GPU
// catalogue and the quota it may still use. Every figure is derived from the
// same list calls the other views use — there is no separate metrics API.
type Read = 'jobs' | 'types' | 'vols' | 'quota'

// CardRead is what a card body shows while its list is still loading, or the
// unavailable note once that list's read has answered without data.
function CardRead({done, unavailable}: {done: boolean, unavailable: string}) {
  return done
    ? <div className="empty"><div className="empty-title">{unavailable}</div></div>
    : <Spinner label={t('common.loading')}/>
}

export function Overview({onOpen, onOpenJob}: {
  onOpen: (area: 'jobs' | 'volumes' | 'resources' | 'images') => void, onOpenJob: (name: string) => void
}) {
  // jobs and types are null until their first read, so a failed read is not shown as an empty list.
  const [jobs, setJobs] = useState<app.Job[] | null>(null)
  const [types, setTypes] = useState<app.InstanceType[] | null>(null)
  // vols, images and quota stay null until their first read, so the figures
  // show a dash instead of a zero or a claim of no usage when a read fails.
  const [vols, setVols] = useState<volumes.Volume[] | null>(null)
  const [images, setImages] = useState<app.ImageRepo[] | null>(null)
  const [imagesFailed, setImagesFailed] = useState(false)
  const [quota, setQuota] = useState<app.QuotaUsage | null>(null)
  // Each card fills in as its own read answers. settled tells a read still in flight
  // (a card shows a spinner) from one that answered without a list (a card says unavailable).
  const [settled, setSettled] = useState<Record<Read, boolean>>({jobs: false, types: false, vols: false, quota: false})
  const [failures, setFailures] = useState<Partial<Record<Read, string>>>({})
  const error = Object.values(failures).find(Boolean) ?? ''
  const onScreen = useActive()

  // track records one read's answer or failure as soon as it arrives, so a slow read
  // does not hold back the cards and banner of the reads that already answered.
  const track = <T,>(key: Read, call: Promise<T>, set: (v: T) => void) => call.then(v => {
    set(v)
    setFailures(f => ({...f, [key]: ''}))
  }, e => {
    setFailures(f => ({...f, [key]: errText(e)}))
    throw e
  }).finally(() => setSettled(s => ({...s, [key]: true})))

  // refresh rejects when any read failed, so RefreshButton can say so; the banner already shows why.
  const refresh = () => Promise.allSettled([
    track('jobs', ListJobs(), setJobs),
    track('types', ListInstanceTypes(), setTypes),
    track('vols', ListVolumes(false), setVols),
    track('quota', Quota(), setQuota),
  ]).then(results => {
    const failed = results.find(r => r.status === 'rejected') as PromiseRejectedResult | undefined
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
    return everyWhileVisible(poll, 8000)
  }, [onScreen])

  // Images cost a Harbor call per repository, so they refresh on a slower beat.
  useEffect(() => {
    if (!onScreen) return
    const load = () => ListImages().then(list => {
      setImages(list)
      setImagesFailed(false)
    }, () => setImagesFailed(true))
    load()
    return everyWhileVisible(load, 60000)
  }, [onScreen])

  const jobList = jobs ?? []
  const typeList = types ?? []
  // Each job family counts by its live attempt, so a retry that runs is counted
  // and the leader it replaced is not.
  const attempts = jobList.map(latestAttempt)
  const running = attempts.filter(j => jobBucket(j.phase) === 'running')
  const queued = attempts.filter(j => jobBucket(j.phase) === 'queued')
  const failed = attempts.filter(j => jobBucket(j.phase) === 'failed')
  // Workers count only jobs that still hold or wait for pods: a finished,
  // failed or stopped job no longer expects its workers to be ready.
  const active = attempts.filter(j => j.phase !== 'Succeeded' && j.phase !== 'Failed' && j.phase !== 'Stopped')
  const readyWorkers = active.reduce((n, j) => n + (j.ready || 0), 0)
  const wantedWorkers = active.reduce((n, j) => n + (j.workers || 0), 0)

  // total and free count instances; the GPU tiles count GPUs, so each GPU type
  // is weighted by its GPUs per instance, as on Resources. CPU-only types have none.
  const gpuTypes = typeList.filter(ty => ty.gpuCount > 0)
  const gpuTotal = gpuTypes.reduce((n, ty) => n + ty.total * ty.gpuCount, 0)
  const gpuFree = gpuTypes.reduce((n, ty) => n + ty.free * ty.gpuCount, 0)
  const gpuUsed = Math.max(0, gpuTotal - gpuFree)

  const storageGiB = (vols ?? []).reduce((n, v) => n + (v.capacityGiB || v.requestedGiB || 0), 0)
  const totalTags = images === null ? null : images.reduce((n, r) => n + tagCount(r), 0)

  return (
    <div className="page">
      <PageHeader eyebrow={t('overview.eyebrow')} title={t('overview.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{t('overview.live')}</span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}

      <div className="metrics">
        <MetricTile icon="play" label={t('overview.runningJobs')} tone="success"
                    value={jobs === null ? '—' : running.length} unit={jobs === null ? undefined : `/ ${jobList.length}`}
                    detail={jobs === null ? '—' : t('overview.workersReady', {ready: readyWorkers, wanted: wantedWorkers})}>
          <Meter value={running.length} max={Math.max(jobList.length, 1)} tone="success"/>
        </MetricTile>
        <MetricTile icon="clock" label={phaseLabel('Queued')} tone="warning"
                    value={jobs === null ? '—' : queued.length}
                    detail={jobs === null ? '—' : queued.length ? t('overview.waitingForQuota') : t('overview.nothingWaiting')}>
          <Meter value={queued.length} max={Math.max(jobList.length, 1)} tone="warning"/>
        </MetricTile>
        <MetricTile icon="warning" label={phaseLabel('Failed')} tone={failed.length ? 'error' : undefined}
                    value={jobs === null ? '—' : failed.length}
                    detail={jobs === null ? '—' : failed.length ? t('overview.openJobs') : t('overview.noFailures')}>
          <Meter value={failed.length} max={Math.max(jobList.length, 1)} tone={failed.length ? 'error' : 'muted'}/>
        </MetricTile>
        <MetricTile icon="cpu" label={t('overview.gpusInUse')} tone="accent"
                    value={types === null ? '—' : gpuUsed} unit={types && gpuTotal ? `/ ${gpuTotal}` : undefined}
                    detail={types === null ? '—' : gpuTotal ? t('overview.freeInCluster', {free: gpuFree}) : t('overview.noGpuNodes')}>
          <Meter value={gpuUsed} max={Math.max(gpuTotal, 1)} tone="accent"/>
        </MetricTile>
      </div>

      <div className="overview-grid">
        <Card title={t('overview.gpuPools')} subtitle={t('overview.gpuPoolsSubtitle')}
              actions={<button className="btn btn-sm btn-ghost" onClick={() => onOpen('resources')}>
                {t('overview.resourcesButton')}<Icon name="chevron" size={13}/>
              </button>}>
          {types === null ? (
            <CardRead done={settled.types} unavailable={t('overview.poolsUnavailable')}/>
          ) : types.length === 0 ? (
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
          {!quota ? <CardRead done={settled.quota} unavailable={t('overview.quotaUnavailable')}/> : (
            <div className="quota-list">
              {(quota.gpuClasses || []).map(g => (
                <Meter key={g.class} value={g.used} max={Math.max(quota.gpuQuota, g.used, 1)}
                       tone={quota.gpuQuota && g.used >= quota.gpuQuota ? 'warning' : 'accent'}
                       label={<span>{g.class === '*' ? t('overview.gpusUnknownClass') : t('overview.gpusOfClass', {cls: g.class})}</span>}
                       right={<span>{g.used}</span>}/>
              ))}
              {quota.gpuQuota > 0 && (quota.gpuClasses || []).length > 0 && (
                <div className="note">{t('overview.gpuShared', {limit: quota.gpuQuota})}</div>
              )}
              {/* A zero or empty limit means unlimited: the bar stays empty rather than full. */}
              <Meter value={toGiB(quota.storageUsed)} max={toGiB(quota.storage)} tone="muted"
                     label={<span>{t('overview.volumeStorage')}</span>}
                     right={<span>{quota.storageUsed || '0'} / {quota.storage || t('overview.unlimited')}</span>}/>
              <Meter value={quota.volumes} max={quota.volumeMax} tone="muted"
                     label={<span>{t('overview.volumes')}</span>}
                     right={<span>{quota.volumes} / {quota.volumeMax || t('overview.unlimited')}</span>}/>
              <Meter value={quota.pods} max={quota.podMax} tone="muted"
                     label={<span>{t('overview.pods')}</span>}
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
          {jobs === null ? (
            <CardRead done={settled.jobs} unavailable={t('overview.jobsUnavailable')}/>
          ) : jobs.length === 0 ? (
            <div className="empty">
              <div className="empty-title">{t('overview.noJobs')}</div>
              <div className="empty-text">{t('overview.createJobHint')}</div>
            </div>
          ) : (
            <table className="table">
              <thead><tr><th>{t('overview.colJob')}</th><th>{t('common.status')}</th><th>{t('overview.colReady')}</th><th>{t('common.created')}</th></tr></thead>
              <tbody>
              {jobs.slice(0, 6).map(j => {
                const live = latestAttempt(j)
                return <tr key={j.name} className="clickable" onClick={() => onOpenJob(j.name)}>
                  <td className="mono trunc" title={j.name}><button type="button" className="row-name">{j.name}</button></td>
                  <td><Status phase={live.phase}/></td>
                  <td className="muted">{live.ready}/{live.workers}</td>
                  <td className="muted">{fmtDate(j.created)}</td>
                </tr>
              })}
              </tbody>
            </table>
          )}
        </Card>

        <Card title={t('overview.storage')} subtitle={t('overview.storageSubtitle')}>
          <div className="stat-row">
            <div className="stat">
              <div className="stat-label">{t('overview.volumes')}</div>
              <div className="stat-value">{vols ? vols.length : '—'}</div>
              <div className="stat-detail">{vols === null ? '—' : storageGiB ? t('overview.allocated', {size: storageGiB}) : t('overview.noneYet')}</div>
              <button className="btn btn-sm btn-ghost" onClick={() => onOpen('volumes')}>
                {t('overview.manage')}<Icon name="chevron" size={13}/>
              </button>
            </div>
            <div className="stat">
              <div className="stat-label">{t('overview.storageQuota')}</div>
              <div className="stat-value">{quota ? (quota.storage || '∞') : '—'}</div>
              <div className="stat-detail">{quota === null ? '—' : quota.storageUsed ? t('overview.storageUsed', {used: quota.storageUsed}) : t('overview.nothingUsed')}</div>
            </div>
            <div className="stat">
              <div className="stat-label">{t('overview.imageTags')}</div>
              <div className="stat-value">{imagesFailed ? '—' : totalTags ?? '—'}</div>
              <div className="stat-detail">{imagesFailed ? t('overview.imagesUnavailable') : images === null ? '—' : images.length === 1 ? t('overview.repositoryOne') : t('overview.repositories', {count: images.length})}</div>
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

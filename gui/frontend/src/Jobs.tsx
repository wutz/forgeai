import {useEffect, useMemo, useRef, useState} from 'react'
import {GetJob, ListJobs, Logs, DeleteJob, StopJob, StartJob, RerunJob} from '../wailsjs/go/app/App'
import {app, training} from '../wailsjs/go/models'
import {failureLabel} from './util'
import {JobCreateDialog} from './JobCreateDialog'
import {TemplateDialog} from './Templates'
import {JobBucket, Status, envToLines, everyWhileVisible, fmtDate, errText, jobBucket, latestAttempt} from './util'
import {t} from './i18n'
import {Badge, Card, Empty, Icon, LogView, PageHeader, Segmented, Spinner, Tabs, tabPanelProps, useActive, useConfirm, useCreateAccess, useDetailClose} from './ui'

type Filter = 'all' | JobBucket

// filterOptions is built per render so the labels follow the language.
const filterOptions = (): {value: Filter, label: string}[] => [
  {value: 'all', label: t('jobs.filterAll')},
  {value: 'running', label: t('jobs.filterRunning')},
  {value: 'queued', label: t('jobs.filterQueued')},
  {value: 'stopped', label: t('jobs.filterStopped')},
  {value: 'failed', label: t('jobs.filterFailed')},
  {value: 'done', label: t('jobs.filterDone')},
]


// settling reports a phase that changes within seconds, so the lists poll faster
// until it passes. Queued can last hours and keeps the normal interval.
const settling = (phase: string) => phase === 'Stopping' || phase === 'Restarting'

// workerCount is the "N workers" text of a job row.
function workerCount(n: number): string {
  return n === 1 ? t('jobs.workersOne') : t('jobs.workersMany', {count: n})
}

// retryCount is the "N retries" text of a job row.
function retryCount(n: number): string {
  return n === 1 ? t('jobs.retriesOne') : t('jobs.retriesMany', {count: n})
}

// retryOnLabel shows a job's retry condition; unknown values are shown as they arrive.
function retryOnLabel(retryOn: string): string {
  if (retryOn === 'Any') return t('jobs.retryAny')
  if (retryOn === 'Infra') return t('jobs.retryInfra')
  return retryOn
}

// focus asks the page to select a job and show all jobs, so the job is in the list.
// Jobs is the view of the project's jobs. The job templates that prefill the
// job form sit under it in the sidebar.
export function Jobs({focus}: {focus?: {name: string} | null}) {
  return (
    <div className="page">
      <JobList focus={focus}/>
    </div>
  )
}

function JobList({focus}: {focus?: {name: string} | null}) {
  // jobs is null until the first read, so a failed first read is not shown as an empty list.
  const [jobs, setJobs] = useState<app.Job[] | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const create = useCreateAccess()
  const [filter, setFilter] = useState<Filter>('all')
  const {split, close: closeDetail} = useDetailClose(selected, setSelected)
  useEffect(() => {
    if (!focus) return
    setFilter('all')
    setSelected(focus.name)
  }, [focus])
  const active = useActive()
  // Each refresh takes a number; a response that is not from the latest refresh
  // is dropped, so a slow older poll cannot overwrite a newer list.
  const latest = useRef(0)

  const refresh = () => {
    const seq = ++latest.current
    ListJobs().then(jobs => {
      if (seq !== latest.current) return
      setJobs(jobs)
      setError('')
    }).catch(e => {
      if (seq === latest.current) setError(errText(e))
    })
  }
  const list = jobs ?? []
  const busy = list.some(j => settling(latestAttempt(j).phase))
  useEffect(() => {
    if (!active) return
    refresh()
    return everyWhileVisible(refresh, busy ? 1500 : 5000)
  }, [active, busy])

  const counts = useMemo(() => {
    const c: Record<Filter, number> = {all: list.length, running: 0, queued: 0, stopped: 0, failed: 0, done: 0}
    for (const j of list) c[jobBucket(latestAttempt(j).phase)]++
    return c
  }, [list])

  const shown = jobs && (filter === 'all' ? jobs : jobs.filter(j => jobBucket(latestAttempt(j).phase) === filter))

  // A filter that hides the open job closes its detail with it: the detail is never for a row the list does not show.
  const chooseFilter = (next: Filter) => {
    setFilter(next)
    const open = list.find(j => j.name === selected)
    if (open && next !== 'all' && jobBucket(latestAttempt(open).phase) !== next) setSelected(null)
  }

  return (
    <>
      <PageHeader eyebrow={t('jobs.eyebrow')} title={t('jobs.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{jobs !== null && t('jobs.total', {count: list.length})}</span>}
                  actions={<>
                    {create.jobs && <button className="btn" onClick={() => setCreating(true)}>
                      <Icon name="plus" size={14}/>{t('jobs.newJob')}
                    </button>}
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && <JobCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        // A new job is queued or running, so a filter that is not All may hide it; the list goes back to All with it open.
        setFilter('all')
        setSelected(name)
        refresh()
      }}/>}

      <div className="list-toolbar">
        {/* Counts are left out until the list is read: zeros would say the cluster has no jobs. */}
        <Segmented options={filterOptions()} value={filter} onChange={chooseFilter} counts={jobs ? counts : undefined}/>
      </div>

      <div className="split" ref={split}>
        <JobTable jobs={shown} failed={!!error} filtered={filter !== 'all'} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {/* Keyed by name: a read still in flight for the previous job lands on an unmounted detail. */}
        {selected && <JobDetail key={selected} name={selected} onDone={refresh} onOpen={n => { setFilter('all'); setSelected(n); refresh() }}
                                onDeleted={() => { setSelected(null); refresh() }}
                                onClose={closeDetail}/>}
      </div>
    </>
  )
}

function JobTable({jobs, failed, filtered, selected, onSelect}: {
  jobs: app.Job[] | null, failed: boolean, filtered: boolean, selected: string | null, onSelect: (name: string) => void
}) {
  // A failed first read leaves nothing to list, so the card is left out under the banner.
  if (!jobs) return failed ? null : <Card><Spinner label={t('common.loading')}/></Card>
  if (!jobs.length) {
    return <Card>{filtered
      ? <Empty icon="play" title={t('jobs.emptyFiltered')}/>
      : <Empty icon="play" title={t('jobs.emptyTitle')}>{t('jobs.emptyHint')}</Empty>}</Card>
  }
  return (
    <Card>
      {/* Beside an open job the table keeps its status, instance and ready columns on one line and
          drops the age: the detail shows when the job was created. */}
      <table className={selected ? 'table jobs-table' : 'table'}>
        <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('common.status')}</th>
          <th>{t('jobs.instance')}</th>
          <th>{t('jobs.colReady')}</th>
          <th>{t('jobs.colRetry')}</th>
          {!selected && <th>{t('jobs.colAge')}</th>}
        </tr>
        </thead>
        <tbody>
        {jobs.map(j => {
          const live = latestAttempt(j)
          return <tr key={j.name} className={selected === j.name ? 'clickable selected' : 'clickable'}
              onClick={() => onSelect(j.name)}>
            <td data-label={t('common.name')} className="mono">
              <button type="button" className="row-name cell-main">{j.name}</button>
              <span className="cell-sub">
                {workerCount(live.workers)}
                {j.retries?.length ? ` · ${retryCount(j.retries.length)}` : ''}
                {j.template ? ` · ${j.template}` : ''}
              </span>
            </td>
            <td data-label={t('common.status')} className="nowrap"><Status phase={live.phase}/>{live.failureReason && live.phase === 'Failed' &&
              <div className="cell-sub" title={live.message}>{failureLabel(live.failureReason)}</div>}</td>
            <td data-label={t('jobs.instance')} className="mono nowrap">{j.instanceType}</td>
            <td data-label={t('jobs.colReady')} className="mono nowrap">{live.ready}/{live.workers}</td>
            <td data-label={t('jobs.colRetry')}>{j.maxRetries
              ? <Badge>{retryOnLabel(j.retryOn || 'Infra')} × {j.maxRetries}</Badge>
              : <span className="muted">—</span>}</td>
            {!selected && <td data-label={t('jobs.colAge')} className="muted">{j.age}</td>}
          </tr>
        })}
        </tbody>
      </table>
    </Card>
  )
}

type DetailTab = 'overview' | 'workers' | 'log' | 'env'

// jobTemplateForm is the job template a job's settings are saved as, mounts
// included: the job carries their paths, as a rerun does.
function jobTemplateForm(job: app.Job) {
  return training.Form.createFrom({
    name: '', description: '', image: job.image, command: job.command,
    env: Object.entries(job.env || {}).map(([name, value]) => ({name, value})),
    instanceType: job.instanceType, workers: job.workers || 1, volumes: job.mounts ?? [],
    maxRetries: job.maxRetries || 0, retryOn: job.retryOn || 'Infra',
  })
}

function JobDetail({name, onDone, onOpen, onDeleted, onClose}: {
  name: string, onDone: () => void, onOpen: (name: string) => void, onDeleted: () => void, onClose: () => void
}) {
  const [job, setJob] = useState<app.Job | null>(null)
  // error is an action's failure; loadError is a failed read. A read clears only
  // its own error, so a failed Stop stays shown until the next action.
  const [error, setError] = useState('')
  const [loadError, setLoadError] = useState('')
  const [tab, setTab] = useState<DetailTab>('overview')
  // null until the first read, so the view can tell "reading" from "no output".
  const [log, setLog] = useState<string | null>(null)
  const [logTitle, setLogTitle] = useState('')
  const [logWorker, setLogWorker] = useState(0)
  const [saving, setSaving] = useState(false)
  const [savedTemplate, setSavedTemplate] = useState('')
  const active = useActive()
  // pending is the action in flight; its button stays disabled until it answers.
  const [pending, setPending] = useState('')
  const confirm = useConfirm()
  // detailSeq numbers the reads, so a slow reply cannot overwrite a newer one.
  const detailSeq = useRef(0)
  const detailOf = (n: string) => {
    const seq = ++detailSeq.current
    return GetJob(n).then(j => {
      if (seq !== detailSeq.current) return
      setJob(j)
      setLoadError('')
    }).catch(e => {
      if (seq === detailSeq.current) setLoadError(errText(e))
    })
  }
  const act = (kind: string, p: Promise<unknown>) => {
    setPending(kind)
    setError('')
    return p.then(() => { onDone(); return detailOf(name) }).catch(e => setError(errText(e))).finally(() => setPending(''))
  }

  useEffect(() => {
    setJob(null)
    setLog(null)
    setLogWorker(0)
    setTab('overview')
    setSaving(false)
    setSavedTemplate('')
    setError('')
    setLoadError('')
    setPending('')
  }, [name])

  // The poll pauses while the view is hidden and restarts with a fresh read.
  const busy = !!job && settling(latestAttempt(job).phase)
  useEffect(() => {
    if (!active) return
    detailOf(name)
    // A failed poll is shown: the job may have been deleted elsewhere, and its actions then fail.
    return everyWhileVisible(() => detailOf(name), busy ? 1500 : 5000)
  }, [name, active, busy])

  const failure = error || loadError
  if (!job) return <Card>{failure
    ? <div className="banner banner-error" role="alert">{failure}</div>
    : <Spinner label={t('jobs.loadingJob')}/>}</Card>

  // A retried job's live attempt is the one to act on; the leader keeps the title.
  const attempt = latestAttempt(job)
  const live = attempt.phase !== 'Failed' && attempt.phase !== 'Succeeded'
  const retriable = attempt.phase === 'Failed' || attempt.phase === 'Succeeded'

  // Rerun submits a fresh job from this one's form and opens it.
  const rerun = () => {
    setPending('rerun')
    setError('')
    RerunJob(attempt.name, '').then(j => onOpen(j.name)).catch(e => setError(errText(e))).finally(() => setPending(''))
  }

  // viewLog rejects when the log cannot be read; the banner shows why.
  const viewLog = (worker: number, label: string) =>
    Logs(attempt.name, worker, 500).then(v => {
      setLog(v)
      setLogTitle(label)
      setLogWorker(worker)
      setTab('log')
    }, e => {
      setError(errText(e))
      throw e
    })

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h2 className="mono">{job.name}</h2>
          <div className="card-sub">{t('jobs.detailSub', {
            template: job.template || t('jobs.defaultRuntime'), created: fmtDate(job.created),
          })}</div>
        </div>
        <div className="card-head-actions">
          <Status phase={attempt.phase}/>
          <button type="button" className="icon-btn" aria-label={t('common.close')} title={t('common.close')} onClick={onClose}>
            <Icon name="close" size={14}/>
          </button>
        </div>
      </div>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {loadError && <div className="banner banner-error" role="alert">{loadError}</div>}
      {savedTemplate && <div className="banner" role="status">{t('jobs.savedTemplate', {name: savedTemplate})}</div>}
      {confirm.dialog}
      {saving && <TemplateDialog form={jobTemplateForm(job)} onClose={() => setSaving(false)} onSaved={n => {
        setSaving(false); setSavedTemplate(n)
      }}/>}

      <div className="detail-actions">
        <button className="btn btn-sm" onClick={() => viewLog(0, t('jobs.workerLabel', {index: 0})).catch(() => {})}>
          <Icon name="scroll" size={13}/>{t('common.logs')}
        </button>
        {live && attempt.phase !== 'Stopped' &&
          <button className="btn btn-sm" disabled={!!pending} onClick={() => confirm.ask({
            title: t('jobs.stopTitle', {name: attempt.name}),
            message: t('jobs.stopMessage'),
            action: t('jobs.stopAction'),
            run: () => act('stop', StopJob(attempt.name)),
          })}>
            <Icon name="square" size={12}/>{pending === 'stop' ? t('jobs.stopping') : t('common.stop')}
          </button>}
        {attempt.phase === 'Stopped' &&
          <button className="btn btn-sm btn-accent" disabled={!!pending} onClick={() => act('start', StartJob(attempt.name))}>
            <Icon name="play" size={12}/>{pending === 'start' ? t('jobs.resuming') : t('common.resume')}
          </button>}
        {retriable &&
          <button className="btn btn-sm" disabled={!!pending} onClick={rerun}>
            <Icon name="refresh" size={13}/>{pending === 'rerun' ? t('jobs.rerunning') : t('jobs.rerun')}
          </button>}
        <button className="btn btn-sm" onClick={() => setSaving(true)}>
          <Icon name="layers" size={13}/>{t('jobs.saveAsTemplate')}
        </button>
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" disabled={!!pending} onClick={() => confirm.ask({
          title: t('jobs.deleteTitle', {name: job.name}),
          message: t('jobs.deleteMessage') + (job.retries?.length
            ? ' ' + t('jobs.deleteRetries', {count: job.retries.length}) : ''),
          action: t('jobs.deleteAction'), danger: true,
          run: () => {
            setPending('delete')
            setError('')
            DeleteJob(job.name).then(onDeleted).catch(e => setError(errText(e))).finally(() => setPending(''))
          },
        })}>
          <Icon name="trash" size={13}/>{pending === 'delete' ? t('jobs.deleting') : t('common.delete')}
        </button>
      </div>

      <div className="section">
        <Tabs idPrefix="job" tabs={[
          {value: 'overview', label: t('jobs.tabOverview'), icon: 'grid'},
          {value: 'workers', label: t('jobs.tabWorkers'), icon: 'cpu'},
          {value: 'log', label: t('jobs.tabLog'), icon: 'scroll'},
          {value: 'env', label: t('jobs.tabEnv'), icon: 'layers'},
        ]} value={tab} onChange={v => {
          setTab(v)
          // Opening the log tab reads the log of the worker it last showed (worker 0 at first).
          if (v === 'log') viewLog(logWorker, logTitle || t('jobs.workerLabel', {index: 0})).catch(() => {})
        }}/>

        <div {...tabPanelProps('job', tab)}>
          {tab === 'overview' && (
            <>
              <dl className="kv">
                <dt>{t('common.image')}</dt><dd className="mono">{job.image}</dd>
                <dt>{t('jobs.instance')}</dt><dd className="mono">{job.instanceType}
                  {job.gpuCount ? ` (${job.gpuCount}× ${job.gpuClass})` : ''}</dd>
                <dt>{t('jobs.workers')}</dt><dd>{t('jobs.workersReady', {ready: attempt.ready, total: attempt.workers})}</dd>
                <dt>{t('jobs.volumes')}</dt><dd className="mono">{job.mounts?.length
                  ? job.mounts.map(m => <div key={`${m.volume}:${m.mountPath}`}>
                      {m.volume} → {m.mountPath}{m.subPath ? ` (${m.subPath})` : ''}{m.readOnly ? ` · ${t('mounts.readOnly')}` : ''}
                    </div>)
                  : job.volumes?.join(', ') || '—'}</dd>
                <dt>{t('jobs.command')}</dt><dd><pre className="code">{job.command || '—'}</pre></dd>
                {attempt.failureReason && <><dt>{t('jobs.failure')}</dt><dd>{t('jobs.failureDetail', {reason: failureLabel(attempt.failureReason), message: attempt.message || ''})}</dd></>}
                {job.retryOf && <><dt>{t('jobs.retryOf')}</dt><dd>
                  <button className="btn btn-sm mono" onClick={() => onOpen(job.retryOf ?? '')}>{job.retryOf}</button>
                </dd></>}
                {job.retriedAs && <><dt>{t('jobs.retriedAs')}</dt><dd>
                  <button className="btn btn-sm mono" onClick={() => onOpen(job.retriedAs ?? '')}>{job.retriedAs}</button>
                </dd></>}
                <dt>{t('common.created')}</dt><dd>{fmtDate(job.created)}</dd>
              </dl>

              {job.retries?.length > 0 && (
                <>
                  <h3 className="sub-head">{t('jobs.retries')}</h3>
                  <div className="mini-table">
                    {job.retries.map(r => (
                      <button key={r.name} className="mini-row" onClick={() => onOpen(r.name)}>
                        <span className="mono">{r.name}</span>
                        <Status phase={r.phase}/>
                        <span className="muted">{r.failureReason ? failureLabel(r.failureReason) : ''}</span>
                        <span className="muted">{fmtDate(r.created)}</span>
                        <Icon name="chevron" size={13}/>
                      </button>
                    ))}
                  </div>
                </>
              )}
            </>
          )}

          {tab === 'workers' && (
            attempt.workerPods?.length ? (
              <div className="mini-table">
                {attempt.workerPods.map(w => (
                  <div key={w.podName} className="mini-row">
                    <span className="mono">{w.podName}</span>
                    <Status phase={w.phase || 'Pending'}/>
                    <span className="mono muted">{w.nodeName || '—'}</span>
                    <span className="muted trunc" title={w.message}>{w.message || ''}</span>
                    <button className="btn btn-sm" onClick={() => viewLog(w.index, w.podName).catch(() => {})}>{t('jobs.logButton')}</button>
                  </div>
                ))}
              </div>
            ) : (
              <div className="empty">
                <div className="empty-title">{attempt.phase === 'Queued' ? t('jobs.noPodsQueued') : t('jobs.noPodsYet')}</div>
              </div>
            )
          )}

          {/* With no log read yet and a read that failed, the log view would wait on that read; retry offers it again. */}
          {tab === 'log' && log === null && error && (
            <div className="actions">
              <button className="btn btn-sm" onClick={() => viewLog(logWorker, logTitle || t('jobs.workerLabel', {index: 0})).catch(() => {})}>
                {t('common.retry')}
              </button>
            </div>
          )}
          {tab === 'log' && (log !== null || !error) && (
            // Keyed by the worker's log so switching workers starts at the end of the new log.
            <LogView key={logTitle} title={logTitle || t('jobs.workerLabel', {index: 0})} text={log}
                     onRefresh={() => viewLog(logWorker, logTitle || t('jobs.workerLabel', {index: 0}))}/>
          )}

          {tab === 'env' && <pre className="code">{envToLines(job.env) || '—'}</pre>}
        </div>
      </div>
    </Card>
  )
}

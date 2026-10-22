import {useEffect, useMemo, useState} from 'react'
import {GetJob, ListJobs, Logs, DeleteJob, StopJob, StartJob, RerunJob} from '../wailsjs/go/app/App'
import {app, training} from '../wailsjs/go/models'
import {JobCreateDialog} from './JobCreateDialog'
import {TemplateDialog} from './Templates'
import {Status, envToLines, fmtDate, errText} from './util'
import {t} from './i18n'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Segmented, Spinner, Tabs, useActive, useConfirm} from './ui'

type Filter = 'all' | 'running' | 'queued' | 'stopped' | 'failed' | 'done'

// filterOptions is built per render so the labels follow the language.
const filterOptions = (): {value: Filter, label: string}[] => [
  {value: 'all', label: t('jobs.filterAll')},
  {value: 'running', label: t('jobs.filterRunning')},
  {value: 'queued', label: t('jobs.filterQueued')},
  {value: 'stopped', label: t('jobs.filterStopped')},
  {value: 'failed', label: t('jobs.filterFailed')},
  {value: 'done', label: t('jobs.filterDone')},
]


function bucket(phase: string): Filter {
  switch (phase) {
    case 'Running':
      return 'running'
    case 'Queued':
    case 'Pending':
    case 'Restarting':
    case 'Stopping':
      return 'queued'
    case 'Stopped':
      return 'stopped'
    case 'Failed':
      return 'failed'
    default:
      return 'done'
  }
}

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

export function Jobs() {
  const [jobs, setJobs] = useState<app.Job[]>([])
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState<Filter>('all')
  const active = useActive()

  const refresh = () => {
    ListJobs().then(jobs => {
      setJobs(jobs)
      setError('')
    }).catch(e => setError(errText(e)))
  }
  useEffect(() => {
    if (!active) return
    refresh()
    const timer = setInterval(refresh, 5000)
    return () => clearInterval(timer)
  }, [active])

  const counts = useMemo(() => {
    const c: Record<Filter, number> = {all: jobs.length, running: 0, queued: 0, stopped: 0, failed: 0, done: 0}
    for (const j of jobs) c[bucket(j.phase)]++
    return c
  }, [jobs])

  const shown = filter === 'all' ? jobs : jobs.filter(j => bucket(j.phase) === filter)

  return (
    <div className="page">
      <PageHeader eyebrow={t('jobs.eyebrow')} title={t('jobs.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{t('jobs.total', {count: jobs.length})}</span>}
                  actions={<button className="btn btn-primary" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>{t('jobs.newJob')}
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && <JobCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}

      <div className="list-toolbar">
        <Segmented options={filterOptions()} value={filter} onChange={setFilter} counts={counts}/>
      </div>

      <div className="split">
        <JobTable jobs={shown} filtered={filter !== 'all'} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {selected && <JobDetail name={selected} onDone={refresh} onOpen={n => { setSelected(n); refresh() }}
                                onDeleted={() => { setSelected(null); refresh() }}/>}
      </div>
    </div>
  )
}

function JobTable({jobs, filtered, selected, onSelect}: {
  jobs: app.Job[], filtered: boolean, selected: string | null, onSelect: (name: string) => void
}) {
  if (!jobs.length) {
    return <Card>{filtered
      ? <Empty icon="play" title={t('jobs.emptyFiltered')}/>
      : <Empty icon="play" title={t('jobs.emptyTitle')}>{t('jobs.emptyHint')}</Empty>}</Card>
  }
  return (
    <Card>
      <table className="table">
        <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('common.status')}</th>
          <th>{t('jobs.instance')}</th>
          <th>{t('jobs.colReady')}</th>
          <th>{t('jobs.colRetry')}</th>
          <th>{t('jobs.colAge')}</th>
        </tr>
        </thead>
        <tbody>
        {jobs.map(j => (
          <tr key={j.name} className={selected === j.name ? 'selected' : ''}
              onClick={() => onSelect(j.name)}>
            <td className="mono">
              <span className="cell-main">{j.name}</span>
              <span className="cell-sub">
                {workerCount(j.workers)}
                {j.retries?.length ? ` · ${retryCount(j.retries.length)}` : ''}
                {j.template ? ` · ${j.template}` : ''}
              </span>
            </td>
            <td><Status phase={j.phase}/>{j.failureReason && j.phase === 'Failed' &&
              <div className="cell-sub" title={j.message}>{j.failureReason}</div>}</td>
            <td className="mono">{j.instanceType}</td>
            <td className="mono">{j.ready}/{j.workers}</td>
            <td>{j.maxRetries
              ? <Badge>{retryOnLabel(j.retryOn || 'Infra')} × {j.maxRetries}</Badge>
              : <span className="muted">—</span>}</td>
            <td className="muted">{j.age}</td>
          </tr>
        ))}
        </tbody>
      </table>
    </Card>
  )
}

type DetailTab = 'overview' | 'workers' | 'log' | 'env'

// jobTemplateForm is the job template a job's settings are saved as. Volumes
// are left out: the job view keeps only claim names, and a template needs the
// mount paths too.
function jobTemplateForm(job: app.Job) {
  return training.Form.createFrom({
    name: '', description: '', image: job.image, command: job.command,
    env: Object.entries(job.env || {}).map(([name, value]) => ({name, value})),
    instanceType: job.instanceType, workers: job.workers || 1, volumes: [],
    maxRetries: job.maxRetries || 0, retryOn: job.retryOn || 'Infra',
  })
}

function JobDetail({name, onDone, onOpen, onDeleted}: {
  name: string, onDone: () => void, onOpen: (name: string) => void, onDeleted: () => void
}) {
  const [job, setJob] = useState<app.Job | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<DetailTab>('overview')
  const [log, setLog] = useState('')
  const [logTitle, setLogTitle] = useState('')
  const [logWorker, setLogWorker] = useState(0)
  const [saving, setSaving] = useState(false)
  const [savedTemplate, setSavedTemplate] = useState('')
  const active = useActive()
  // pending is the action in flight; its button stays disabled until it answers.
  const [pending, setPending] = useState('')
  const confirm = useConfirm()
  const detailOf = (n: string) => GetJob(n).then(setJob).catch(e => setError(errText(e)))
  const act = (kind: string, p: Promise<unknown>) => {
    setPending(kind)
    setError('')
    return p.then(() => { onDone(); return detailOf(name) }).catch(e => setError(errText(e))).finally(() => setPending(''))
  }

  useEffect(() => {
    setJob(null)
    setLog('')
    setLogWorker(0)
    setTab('overview')
    setSaving(false)
    setSavedTemplate('')
    setError('')
    setPending('')
  }, [name])

  // The poll pauses while the view is hidden and restarts with a fresh read.
  useEffect(() => {
    if (!active) return
    GetJob(name).then(setJob).catch(e => setError(errText(e)))
    // A failed poll is shown: the job may have been deleted elsewhere, and its actions then fail.
    const timer = setInterval(() => GetJob(name).then(setJob).catch(e => setError(errText(e))), 5000)
    return () => clearInterval(timer)
  }, [name, active])

  if (!job) return <Card>{error
    ? <div className="banner banner-error" role="alert">{error}</div>
    : <Spinner label={t('jobs.loadingJob')}/>}</Card>

  const live = job.phase !== 'Failed' && job.phase !== 'Succeeded'
  const retriable = job.phase === 'Failed' || job.phase === 'Succeeded'

  // Rerun submits a fresh job from this one's form and opens it.
  const rerun = () => {
    setPending('rerun')
    setError('')
    RerunJob(job.name, '').then(j => onOpen(j.name)).catch(e => setError(errText(e))).finally(() => setPending(''))
  }

  // viewLog rejects when the log cannot be read; the banner shows why.
  const viewLog = (worker: number, label: string) =>
    Logs(job.name, worker, 500).then(v => {
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
          <h3 className="mono">{job.name}</h3>
          <div className="card-sub">{t('jobs.detailSub', {
            template: job.template || t('jobs.defaultRuntime'), created: fmtDate(job.created),
          })}</div>
        </div>
        <div className="card-head-actions"><Status phase={job.phase}/></div>
      </div>
      {error && <div className="banner banner-error">{error}</div>}
      {savedTemplate && <div className="banner" role="status">{t('jobs.savedTemplate', {name: savedTemplate})}</div>}
      {confirm.dialog}
      {saving && <TemplateDialog form={jobTemplateForm(job)} onClose={() => setSaving(false)} onSaved={n => {
        setSaving(false); setSavedTemplate(n)
      }}/>}

      <div className="detail-actions">
        <button className="btn btn-sm" onClick={() => viewLog(0, t('jobs.workerLabel', {index: 0})).catch(() => {})}>
          <Icon name="scroll" size={13}/>{t('common.logs')}
        </button>
        {live && job.phase !== 'Stopped' &&
          <button className="btn btn-sm" disabled={!!pending} onClick={() => confirm.ask({
            title: t('jobs.stopTitle', {name: job.name}),
            message: t('jobs.stopMessage'),
            action: t('jobs.stopAction'),
            run: () => act('stop', StopJob(job.name)),
          })}>
            <Icon name="square" size={12}/>{pending === 'stop' ? t('jobs.stopping') : t('common.stop')}
          </button>}
        {job.phase === 'Stopped' &&
          <button className="btn btn-sm btn-accent" disabled={!!pending} onClick={() => act('start', StartJob(job.name))}>
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
        <button className="btn btn-sm btn-danger" onClick={() => confirm.ask({
          title: t('jobs.deleteTitle', {name: job.name}),
          message: t('jobs.deleteMessage') + (job.retries?.length
            ? ' ' + t('jobs.deleteRetries', {count: job.retries.length}) : ''),
          action: t('jobs.deleteAction'), danger: true,
          run: () => DeleteJob(job.name).then(onDeleted).catch(e => setError(errText(e))),
        })}>
          <Icon name="trash" size={13}/>{t('common.delete')}
        </button>
      </div>

      <div className="section">
        <Tabs tabs={[
          {value: 'overview', label: t('jobs.tabOverview'), icon: 'grid'},
          {value: 'workers', label: t('jobs.tabWorkers'), icon: 'cpu'},
          {value: 'log', label: t('jobs.tabLog'), icon: 'scroll'},
          {value: 'env', label: t('jobs.tabEnv'), icon: 'layers'},
        ]} value={tab} onChange={v => {
          setTab(v)
          // Opening the log tab reads the log of the worker it last showed (worker 0 at first).
          if (v === 'log') viewLog(logWorker, logTitle || t('jobs.workerLabel', {index: 0})).catch(() => {})
        }}/>

        {tab === 'overview' && (
          <>
            <dl className="kv">
              <dt>{t('common.image')}</dt><dd className="mono">{job.image}</dd>
              <dt>{t('jobs.instance')}</dt><dd className="mono">{job.instanceType}
                {job.gpuCount ? ` (${job.gpuCount}× ${job.gpuClass})` : ''}</dd>
              <dt>{t('jobs.workers')}</dt><dd>{t('jobs.workersReady', {ready: job.ready, total: job.workers})}</dd>
              <dt>{t('jobs.volumes')}</dt><dd className="mono">{job.volumes?.join(', ') || '—'}</dd>
              <dt>{t('jobs.command')}</dt><dd><pre className="code">{job.command || '—'}</pre></dd>
              {job.failureReason && <><dt>{t('jobs.failure')}</dt><dd>{t('jobs.failureDetail', {reason: job.failureReason, message: job.message || ''})}</dd></>}
              {job.retriedAs && <><dt>{t('jobs.retriedAs')}</dt><dd>
                <button className="btn btn-sm mono" onClick={() => onOpen(job.retriedAs ?? '')}>{job.retriedAs}</button>
              </dd></>}
              <dt>{t('common.created')}</dt><dd>{fmtDate(job.created)}</dd>
            </dl>

            {job.retries?.length > 0 && (
              <>
                <h4 className="sub-head">{t('jobs.retries')}</h4>
                <div className="mini-table">
                  {job.retries.map(r => (
                    <button key={r.name} className="mini-row" onClick={() => onOpen(r.name)}>
                      <span className="mono">{r.name}</span>
                      <Status phase={r.phase}/>
                      <span className="muted">{r.failureReason || ''}</span>
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
          job.workerPods?.length ? (
            <div className="mini-table">
              {job.workerPods.map(w => (
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
              <div className="empty-title">{job.phase === 'Queued' ? t('jobs.noPodsQueued') : t('jobs.noPodsYet')}</div>
            </div>
          )
        )}

        {tab === 'log' && (
          <>
            <div className="log-bar">
              <span className="mono muted">{logTitle || t('jobs.workerLabel', {index: 0})}</span>
              <span className="spacer"/>
              <RefreshButton label="Reload" onRefresh={() => viewLog(logWorker, logTitle || t('jobs.workerLabel', {index: 0}))}/>
            </div>
            <pre className="logs">{log || t('jobs.noOutput')}</pre>
          </>
        )}

        {tab === 'env' && <pre className="code">{envToLines(job.env) || '—'}</pre>}
      </div>
    </Card>
  )
}

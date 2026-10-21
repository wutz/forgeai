import {useEffect, useMemo, useState} from 'react'
import {GetJob, ListJobs, Logs, DeleteJob, StopJob, StartJob, RerunJob} from '../wailsjs/go/app/App'
import {app, training} from '../wailsjs/go/models'
import {JobCreateDialog} from './JobCreateDialog'
import {TemplateDialog} from './Templates'
import {Status, envToLines, fmtDate, errText} from './util'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Segmented, Spinner, Tabs, useConfirm} from './ui'

type Filter = 'all' | 'running' | 'queued' | 'stopped' | 'failed' | 'done'

const FILTERS: {value: Filter, label: string}[] = [
  {value: 'all', label: 'All'},
  {value: 'running', label: 'Running'},
  {value: 'queued', label: 'Queued'},
  {value: 'stopped', label: 'Stopped'},
  {value: 'failed', label: 'Failed'},
  {value: 'done', label: 'Done'},
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

export function Jobs() {
  const [jobs, setJobs] = useState<app.Job[]>([])
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState<Filter>('all')

  const refresh = () => {
    ListJobs().then(jobs => {
      setJobs(jobs)
      setError('')
    }).catch(e => setError(errText(e)))
  }
  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 5000)
    return () => clearInterval(t)
  }, [])

  const counts = useMemo(() => {
    const c: Record<Filter, number> = {all: jobs.length, running: 0, queued: 0, stopped: 0, failed: 0, done: 0}
    for (const j of jobs) c[bucket(j.phase)]++
    return c
  }, [jobs])

  const shown = filter === 'all' ? jobs : jobs.filter(j => bucket(j.phase) === filter)

  return (
    <div className="page">
      <PageHeader eyebrow="Workspace" title="Jobs"
                  meta={<span className="muted" style={{fontSize: 12.5}}>{jobs.length} total</span>}
                  actions={<button className="btn btn-primary" onClick={() => setCreating(true)}>
                    <Icon name="plus" size={14}/>New job
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && <JobCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}

      <div className="list-toolbar">
        <Segmented options={FILTERS} value={filter} onChange={setFilter} counts={counts}/>
      </div>

      <div className="split">
        <JobTable jobs={shown} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {selected && <JobDetail name={selected} onDone={refresh} onOpen={n => { setSelected(n); refresh() }}
                                onDeleted={() => { setSelected(null); refresh() }}/>}
      </div>
    </div>
  )
}

function JobTable({jobs, selected, onSelect}: {
  jobs: app.Job[], selected: string | null, onSelect: (name: string) => void
}) {
  if (!jobs.length) {
    return <Card><Empty icon="play" title="No jobs here">Create one to start training; jobs
      wait in the Kueue queue until GPU quota is free.</Empty></Card>
  }
  return (
    <Card>
      <table className="table">
        <thead>
        <tr>
          <th>Name</th>
          <th>Status</th>
          <th>Instance</th>
          <th>Ready</th>
          <th>Retry</th>
          <th>Age</th>
        </tr>
        </thead>
        <tbody>
        {jobs.map(j => (
          <tr key={j.name} className={selected === j.name ? 'selected' : ''}
              onClick={() => onSelect(j.name)}>
            <td className="mono">
              <span className="cell-main">{j.name}</span>
              <span className="cell-sub">
                {j.workers} worker{j.workers === 1 ? '' : 's'}
                {j.retries?.length ? ` · ${j.retries.length} retries` : ''}
                {j.template ? ` · ${j.template}` : ''}
              </span>
            </td>
            <td><Status phase={j.phase}/>{j.failureReason && j.phase === 'Failed' &&
              <div className="cell-sub" title={j.message}>{j.failureReason}</div>}</td>
            <td className="mono">{j.instanceType}</td>
            <td className="mono">{j.ready}/{j.workers}</td>
            <td>{j.maxRetries
              ? <Badge>{j.retryOn || 'Infra'} × {j.maxRetries}</Badge>
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
// are left out: job templates do not carry them.
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
    GetJob(name).then(setJob).catch(e => setError(errText(e)))
    const t = setInterval(() => GetJob(name).then(setJob).catch(() => {}), 5000)
    return () => clearInterval(t)
  }, [name])

  if (!job) return <Card><Spinner label={error || 'Loading job…'}/></Card>
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
          <div className="card-sub">{job.template || 'default runtime'} · created {fmtDate(job.created)}</div>
        </div>
        <div className="card-head-actions"><Status phase={job.phase}/></div>
      </div>
      {error && <div className="banner banner-error">{error}</div>}
      {savedTemplate && <div className="banner" role="status">Saved job template {savedTemplate}.</div>}
      {confirm.dialog}
      {saving && <TemplateDialog form={jobTemplateForm(job)} onClose={() => setSaving(false)} onSaved={n => {
        setSaving(false); setSavedTemplate(n)
      }}/>}

      <div className="detail-actions">
        <button className="btn btn-sm" onClick={() => viewLog(0, 'worker 0').catch(() => {})}>
          <Icon name="scroll" size={13}/>Logs
        </button>
        {live && job.phase !== 'Stopped' &&
          <button className="btn btn-sm" disabled={!!pending} onClick={() => act('stop', StopJob(job.name))}>
            <Icon name="square" size={12}/>{pending === 'stop' ? 'Stopping…' : 'Stop'}
          </button>}
        {job.phase === 'Stopped' &&
          <button className="btn btn-sm btn-accent" disabled={!!pending} onClick={() => act('start', StartJob(job.name))}>
            <Icon name="play" size={12}/>{pending === 'start' ? 'Resuming…' : 'Resume'}
          </button>}
        {retriable &&
          <button className="btn btn-sm" disabled={!!pending} onClick={rerun}>
            <Icon name="refresh" size={13}/>{pending === 'rerun' ? 'Rerunning…' : 'Rerun'}
          </button>}
        <button className="btn btn-sm" onClick={() => setSaving(true)}>
          <Icon name="layers" size={13}/>Save as template
        </button>
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" onClick={() => confirm.ask({
          title: `Delete job ${job.name}?`,
          message: 'Its worker pods stop and the logs go with them. Kueue releases the quota. This cannot be undone.',
          action: 'Delete job', danger: true,
          run: () => DeleteJob(job.name).then(onDeleted).catch(e => setError(errText(e))),
        })}>
          <Icon name="trash" size={13}/>Delete
        </button>
      </div>

      <div className="section">
        <Tabs tabs={[
          {value: 'overview', label: 'Overview', icon: 'grid'},
          {value: 'workers', label: 'Workers', icon: 'cpu'},
          {value: 'log', label: 'Log', icon: 'scroll'},
          {value: 'env', label: 'Env', icon: 'layers'},
        ]} value={tab} onChange={setTab}/>

        {tab === 'overview' && (
          <>
            <dl className="kv">
              <dt>Image</dt><dd className="mono">{job.image}</dd>
              <dt>Instance</dt><dd className="mono">{job.instanceType}
                {job.gpuCount ? ` (${job.gpuCount}× ${job.gpuClass})` : ''}</dd>
              <dt>Workers</dt><dd>{job.ready} ready of {job.workers}</dd>
              <dt>Volumes</dt><dd className="mono">{job.volumes?.join(', ') || '—'}</dd>
              <dt>Command</dt><dd><pre className="code">{job.command || '—'}</pre></dd>
              {job.failureReason && <><dt>Failure</dt><dd>{job.failureReason}: {job.message}</dd></>}
              {job.retriedAs && <><dt>Retried as</dt><dd className="mono">{job.retriedAs}</dd></>}
              <dt>Created</dt><dd>{fmtDate(job.created)}</dd>
            </dl>

            {job.retries?.length > 0 && (
              <>
                <h4 className="sub-head">Retries</h4>
                <div className="mini-table">
                  {job.retries.map(r => (
                    <button key={r.name} className="mini-row" onClick={() => detailOf(r.name)}>
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
                  <button className="btn btn-sm" onClick={() => viewLog(w.index, w.podName).catch(() => {})}>Log</button>
                </div>
              ))}
            </div>
          ) : (
            <div className="empty">
              <div className="empty-title">No pods {job.phase === 'Queued' ? '— waiting for quota' : 'yet'}</div>
            </div>
          )
        )}

        {tab === 'log' && (
          <>
            <div className="log-bar">
              <span className="mono muted">{logTitle || 'worker 0'}</span>
              <span className="spacer"/>
              <RefreshButton label="Reload" onRefresh={() => viewLog(logWorker, logTitle || 'worker 0')}/>
            </div>
            <pre className="logs">{log || 'No output'}</pre>
          </>
        )}

        {tab === 'env' && <pre className="code">{envToLines(job.env) || '—'}</pre>}
      </div>
    </Card>
  )
}

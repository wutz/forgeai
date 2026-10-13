import {useEffect, useState} from 'react'
import {GetJob, ListJobs, Logs, DeleteJob, StopJob, StartJob, RerunJob} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {JobCreateDialog} from './JobCreateDialog'
import {Status, envToLines, fmtDate, errText} from './util'

export function Jobs() {
  const [jobs, setJobs] = useState<app.Job[]>([])
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

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

  return (
    <>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Jobs</h1>
        <span className="muted">{jobs.length}</span>
        <span className="spacer"/>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>New job</button>
      </div>
      {creating && <JobCreateDialog onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        setSelected(name)
        refresh()
      }}/>}
      <div className="split">
        <JobTable jobs={jobs} selected={selected} onSelect={n =>
          setSelected(prev => prev === n ? null : n)}/>
        {selected && <JobDetail name={selected} onDone={refresh}/>}
      </div>
    </>
  )
}

function JobTable({jobs, selected, onSelect}: {
  jobs: app.Job[], selected: string | null, onSelect: (name: string) => void
}) {
  if (!jobs.length) return <div className="card"><div className="empty">No jobs yet. Create one above.</div></div>
  return (
    <div className="card">
      <table className="table">
        <thead>
        <tr>
          <th>Name</th>
          <th>Status</th>
          <th>Instance</th>
          <th>Workers</th>
          <th>Ready</th>
          <th>Retry</th>
          <th>Age</th>
        </tr>
        </thead>
        <tbody>
        {jobs.map(j => (
          <tr key={j.name} className={selected === j.name ? 'selected' : ''}
              onClick={() => onSelect(j.name)}>
            <td className="mono">{j.name}
              {j.retries?.length ? <span className="muted"> ·{j.retries.length}</span> : null}
            </td>
            <td><Status phase={j.phase}/>{j.failureReason && j.phase === 'Failed' &&
              <span className="muted" title={j.message}> {j.failureReason}</span>}</td>
            <td className="mono">{j.instanceType}</td>
            <td>{j.workers}</td>
            <td>{j.ready}/{j.workers}</td>
            <td>{j.maxRetries ? `${j.retryOn||'Infra'}×${j.maxRetries}` : '—'}</td>
            <td>{j.age}</td>
          </tr>
        ))}
        </tbody>
      </table>
    </div>
  )
}

function JobDetail({name, onDone}: { name: string, onDone: () => void }) {
  const [job, setJob] = useState<app.Job | null>(null)
  const detailOf = (n: string) => GetJob(n).then(setJob).catch(e => setError(errText(e)))
  const [error, setError] = useState('')
  const [tab, setTab] = useState<'spec' | 'pods' | 'log'>('spec')
  const [log, setLog] = useState('')
  const act = (p: Promise<unknown>) => p.then(onDone).catch(e => setError(errText(e)))

  useEffect(() => {
    GetJob(name).then(setJob).catch(e => setError(errText(e)))
    const t = setInterval(() => GetJob(name).then(setJob).catch(() => {}), 5000)
    return () => clearInterval(t)
  }, [name])

  if (!job) return <div className="card"><div className="empty">{error || '…'}</div></div>
  const live = job.phase !== 'Failed' && job.phase !== 'Succeeded'
  const retriable = job.phase === 'Failed' || job.phase === 'Succeeded'

  return (
    <div className="card detail">
      <div className="card-head">
        <h3 className="mono">{job.name}</h3>
        <Status phase={job.phase}/>
        <span className="spacer"/>
        <button className="btn btn-sm" onClick={() =>
          Logs(job.name, 0, 500).then(v => { setLog(v); setTab('log') }).catch(e => setError(errText(e)))}>
          Logs
        </button>
        {live && job.phase !== 'Stopped' &&
          <button className="btn btn-sm" onClick={() => act(StopJob(job.name))}>Stop</button>}
        {(job.phase === 'Stopped') &&
          <button className="btn btn-sm btn-primary" onClick={() => act(StartJob(job.name))}>Resume</button>}
      </div>
      {error && <div className="banner banner-error">{error}</div>}

      <div className="section">
        <dl className="kv">
          <dt>Image</dt><dd className="mono">{job.image}</dd>
          <dt>Instance</dt><dd className="mono">{job.instanceType}
            {job.gpuCount ? ` (${job.gpuCount}× ${job.gpuClass})` : ''}</dd>
          <dt>Workers</dt><dd>{job.workers}</dd>
          <dt>Template</dt><dd>{job.template || '—'}</dd>
          <dt>Volumes</dt><dd className="mono">{job.volumes?.join(', ') || '—'}</dd>
          <dt>Command</dt>
          <dd><pre className="code">{job.command || '—'}</pre></dd>
          {job.failureReason && <>
            <dt>Failure</dt><dd>{job.failureReason}: {job.message}</dd>
          </>}
          {job.retriedAs && <>
            <dt>Retried as</dt><dd className="mono">{job.retriedAs}</dd>
          </>}
          <dt>Created</dt><dd>{fmtDate(job.created)}</dd>
        </dl>
        <div className="actions" style={{marginTop: 12}}>
          {retriable && <button className="btn btn-sm"
            onClick={() => act(RerunJob(job.name, ''))}>Rerun</button>}
          <button className="btn btn-sm btn-danger"
            onClick={() => confirm(`Delete job ${job.name}?`) && act(DeleteJob(job.name))}>Delete</button>
        </div>
      </div>

      {job.retries?.length > 0 && (
        <div className="section">
          <h4>Retries</h4>
          <table className="table">
            <tbody>
            {job.retries.map(r => <tr key={r.name} onClick={() => detailOf(r.name)}>
              <td className="mono">{r.name}</td>
              <td><Status phase={r.phase}/></td>
              <td className="muted">{r.failureReason || ''}</td>
              <td className="muted">{fmtDate(r.created)}</td>
            </tr>)}
            </tbody>
          </table>
        </div>
      )}

      <div className="section">
        <div className="tabs">
          <button className={`btn btn-sm ${tab === 'pods' ? 'btn-primary' : ''}`}
                  onClick={() => setTab('pods')}>Workers</button>
          <button className={`btn btn-sm ${tab === 'spec' ? 'btn-primary' : ''}`}
                  onClick={() => setTab('spec')}>Env</button>
          {log && <button className={`btn btn-sm ${tab === 'log' ? 'btn-primary' : ''}`}
                  onClick={() => setTab('log')}>Log</button>}
        </div>
        {tab === 'log' && <pre className="logs">{log || 'No output'}</pre>}
        {tab === 'pods' && (
          <table className="table">
            <tbody>
            {job.workerPods?.length ? job.workerPods.map(w => (
              <tr key={w.podName}>
                <td className="mono">{w.podName}</td>
                <td><Status phase={w.phase || 'Pending'}/></td>
                <td className="mono">{w.nodeName || '—'}</td>
                <td className="muted">{w.message || ''}</td>
                <td>
                  <button className="btn btn-sm" onClick={() =>
                    Logs(job.name, w.index, 500).then(setLog).catch(e => setError(errText(e)))}>
                  View log
                  </button>
                </td>
              </tr>
            )) : <tr><td className="muted">No pods {job.phase === 'Queued' ? '(waiting for quota)' : 'yet'}</td></tr>}
            </tbody>
          </table>
        )}
        {tab === 'spec' && <pre className="code">{envToLines(job.env) || '—'}</pre>}
      </div>
    </div>
  )
}

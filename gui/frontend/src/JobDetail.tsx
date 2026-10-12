import {useEffect, useState} from 'react'
import {DeleteJob, Logs, RestartJob, StartJob, StopJob} from '../wailsjs/go/main/App'
import {main} from '../wailsjs/go/models'
import {errText, Status} from './util'

type Props = {
  job: main.Job
  onClose: () => void
  onChanged: () => void
  onDeleted: () => void
}

const active = ['Pending', 'Queued', 'Running', 'Restarting']

export function JobDetail({job, onClose, onChanged, onDeleted}: Props) {
  const [worker, setWorker] = useState(0)
  const [logs, setLogs] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => { setWorker(0); setError('') }, [job.name])

  useEffect(() => {
    let stop = false
    const load = async () => {
      try {
        const text = await Logs(job.name, worker, 500)
        if (!stop) setLogs(text)
      } catch (e) {
        if (!stop) setLogs(errText(e))
      }
    }
    load()
    const t = setInterval(load, 5000)
    return () => { stop = true; clearInterval(t) }
  }, [job.name, worker])

  const run = async (fn: (name: string) => Promise<unknown>, after = onChanged) => {
    setBusy(true)
    try {
      await fn(job.name)
      setError('')
      after()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const remove = () => {
    if (confirm(`Delete job ${job.jobID}? This removes its pods.`)) run(DeleteJob, onDeleted)
  }

  const isActive = active.includes(job.phase)

  return (
    <aside className="card" aria-label={`Job ${job.jobID}`}>
      <div className="card-head">
        <h3 className="mono">{job.jobID}</h3>
        <Status phase={job.phase}/>
        <span className="spacer"/>
        <button className="btn btn-sm" onClick={onClose} aria-label="Close details">✕</button>
      </div>
      <div className="card-body">
        {error && <div className="banner banner-error" role="alert">{error}</div>}
        {job.message && <div className="banner banner-warn">{job.message}</div>}
        <div className="actions">
          {isActive
            ? <button className="btn btn-sm" disabled={busy} onClick={() => run(StopJob)}>Stop</button>
            : <button className="btn btn-sm" disabled={busy} onClick={() => run(StartJob)}>Start</button>}
          <button className="btn btn-sm" disabled={busy || job.suspended} onClick={() => run(RestartJob)}>Restart</button>
          <button className="btn btn-sm btn-danger" disabled={busy} onClick={remove}>Delete</button>
        </div>
      </div>
      <div className="section">
        <dl className="kv">
          <dt>Name</dt><dd className="mono">{job.name}</dd>
          <dt>User</dt><dd className="mono">{job.userID}</dd>
          <dt>Image</dt><dd className="mono">{job.image}</dd>
          <dt>Command</dt><dd className="mono">{job.command || '—'}</dd>
          <dt>Resource</dt><dd className="mono">{job.resourceID} × {job.workers}</dd>
          <dt>Started</dt><dd>{job.started ? new Date(job.started).toLocaleString() : '—'}</dd>
          <dt>Duration</dt><dd>{job.duration || '—'}</dd>
        </dl>
      </div>
      <div className="section">
        <h4>Workers</h4>
        {job.workerPods.length === 0 ? <div className="muted">No pods yet.</div> : (
          <dl className="kv">
            {job.workerPods.map(w => (
              <div key={w.index} style={{display: 'contents'}}>
                <dt>worker-{w.index}</dt>
                <dd>
                  <Status phase={w.phase || 'Pending'}/> <span className="muted mono">{w.nodeName}</span>
                </dd>
              </div>
            ))}
          </dl>
        )}
      </div>
      <div className="section">
        <div className="tabs">
          <h4>Logs</h4>
          <span className="spacer"/>
          {job.workers > 1 && (
            <select className="select" aria-label="Worker" value={worker} onChange={e => setWorker(Number(e.target.value))}>
              {Array.from({length: job.workers}, (_, i) => <option key={i} value={i}>worker-{i}</option>)}
            </select>
          )}
        </div>
        <pre className="logs">{logs || 'No output.'}</pre>
      </div>
    </aside>
  )
}

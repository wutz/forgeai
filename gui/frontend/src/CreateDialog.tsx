import {FormEvent, useEffect, useState} from 'react'
import {CreateJob} from '../wailsjs/go/main/App'
import {main} from '../wailsjs/go/models'
import {errText} from './util'

type Props = {
  resources: main.ResourceInfo[]
  userID: string
  onClose: () => void
  onCreated: (j: main.Job) => void
}

function parseEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const i = line.indexOf('=')
    if (i > 0) env[line.slice(0, i).trim()] = line.slice(i + 1)
  }
  return env
}

export function CreateDialog({resources, userID, onClose, onCreated}: Props) {
  const [form, setForm] = useState({
    jobID: '', userID, image: 'pytorch/pytorch:2.10.0-cuda12.8-cudnn9-runtime',
    command: 'nvidia-smi && sleep infinity', resourceID: resources[0]?.id || '',
    poolID: 'default', workers: 1, env: '', ssh: true, jupyter: true,
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k: string, v: unknown) => setForm(f => ({...f, [k]: v}))

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const job = await CreateJob(main.JobForm.createFrom({
        ...form, workers: Number(form.workers), env: parseEnv(form.env),
      }))
      onCreated(job)
    } catch (err) {
      setError(errText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="overlay" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="create-title">
        <div className="card-head"><h3 id="create-title">New training job</h3></div>
        <form id="create-form" onSubmit={submit}>
          {error && <div className="banner banner-error" role="alert">{error}</div>}
          <div className="row">
            <div className="field">
              <label htmlFor="f-user">User ID</label>
              <input id="f-user" className="input" required value={form.userID} onChange={e => set('userID', e.target.value)}/>
            </div>
            <div className="field">
              <label htmlFor="f-id">Job ID</label>
              <input id="f-id" className="input mono" value={form.jobID} placeholder="random"
                     pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?" maxLength={40} onChange={e => set('jobID', e.target.value)}/>
            </div>
          </div>
          <div className="field">
            <label htmlFor="f-image">Image</label>
            <input id="f-image" className="input mono" required value={form.image} onChange={e => set('image', e.target.value)}/>
          </div>
          <div className="field">
            <label htmlFor="f-cmd">Command</label>
            <textarea id="f-cmd" className="textarea" rows={3} value={form.command} onChange={e => set('command', e.target.value)}/>
            <span className="hint">Runs with bash -c. Multi-worker jobs get torchrun-style env (MASTER_ADDR, WORLD_SIZE, …).</span>
          </div>
          <div className="row">
            <div className="field" style={{flex: 2}}>
              <label htmlFor="f-res">Resource</label>
              <select id="f-res" className="select" required value={form.resourceID} onChange={e => set('resourceID', e.target.value)}>
                {resources.map(r => (
                  <option key={r.id} value={r.id}>
                    {r.id} — {r.gpus ? `${r.gpus}× ${r.gpuModel || 'GPU'}, ` : ''}{r.cpu} CPU, {r.memoryGB} GB
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="f-workers">Workers</label>
              <input id="f-workers" className="input" type="number" min={1} max={64} value={form.workers} onChange={e => set('workers', e.target.value)}/>
            </div>
            <div className="field">
              <label htmlFor="f-pool">Pool</label>
              <input id="f-pool" className="input" value={form.poolID} onChange={e => set('poolID', e.target.value)}/>
            </div>
          </div>
          <div className="field">
            <label htmlFor="f-env">Environment</label>
            <textarea id="f-env" className="textarea" rows={3} placeholder="KEY=value, one per line" value={form.env} onChange={e => set('env', e.target.value)}/>
          </div>
          <div className="row" style={{flex: 'none'}}>
            <label className="check"><input type="checkbox" checked={form.ssh} onChange={e => set('ssh', e.target.checked)}/>SSH access</label>
            <label className="check"><input type="checkbox" checked={form.jupyter} onChange={e => set('jupyter', e.target.checked)}/>Jupyter access</label>
          </div>
        </form>
        <div className="dialog-foot">
          <button className="btn" type="button" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" type="submit" form="create-form" disabled={busy}>{busy ? 'Creating…' : 'Create job'}</button>
        </div>
      </div>
    </div>
  )
}

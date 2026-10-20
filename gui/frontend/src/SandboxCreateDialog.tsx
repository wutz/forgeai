import {FormEvent, useEffect, useState} from 'react'
import {CreateSandbox, ListInstanceTypes, ListVolumes, SandboxOptions, SandboxTemplates} from '../wailsjs/go/app/App'
import {app, sandbox, volumes, v1} from '../wailsjs/go/models'
import {envToLines, linesToEnv, errText} from './util'
import {Dialog} from './ui'

// SandboxCreateDialog is the "New sandbox" form: image, isolation runtime
// class, optional instance type and volumes, and the auto-stop timer.
export function SandboxCreateDialog({onClose, onCreated}: {
  onClose: () => void, onCreated: (name: string) => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [opts, setOpts] = useState<app.SandboxOptions | null>(null)
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [vols, setVols] = useState<volumes.Volume[]>([])
  const [f, setF] = useState<sandbox.Form>(sandbox.Form.createFrom({
    name: '', image: '', command: '', env: [], instanceType: '', runtimeClass: '', service: true,
    volumes: [], ttl: v1.Duration.createFrom({Duration: 0}),
  }))
  const [envText, setEnvText] = useState('')
  const [mounts, setMounts] = useState('')
  const [ttlHours, setTtlHours] = useState(0)
  const [templates, setTemplates] = useState<app.SandboxTemplate[]>([])

  useEffect(() => {
    SandboxOptions().then(o => {
      setOpts(o)
      setF(prev => sandbox.Form.createFrom({
        ...prev, image: o.defaultImage, runtimeClass: o.defaultRuntimeClass,
      }))
    }).catch(e => setError(errText(e)))
    ListInstanceTypes().then(setTypes).catch(() => {})
    ListVolumes(false).then(setVols).catch(() => {})
    SandboxTemplates().then(setTemplates).catch(() => {})
  }, [])

  const set = <K extends keyof sandbox.Form>(k: K, v: sandbox.Form[K]) =>
    setF(prev => sandbox.Form.createFrom({...prev, [k]: v}))

  // useTemplate prefills the agent fields from a SandboxTemplate; the fields
  // stay editable afterwards.
  const useTemplate = (name: string) => {
    const form = templates.find(t => t.name === name)?.form
    set('template', name)
    if (!form) return
    setF(prev => sandbox.Form.createFrom({
      ...prev,
      template: name,
      image: form.image,
      command: form.command || '',
      runtimeClass: form.runtimeClass || '',
    }))
    setEnvText(envToLines(Object.fromEntries((form.env || []).map(e => [e.name, e.value]))))
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    const form = sandbox.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      volumes: mounts.split('\n').map(line => {
        const [volume, path, sub] = line.trim().split(/\s+/)
        if (!volume || !path) return null
        return {volume, mountPath: path, subPath: sub || '', readOnly: false}
      }).filter(Boolean),
      // The form collects hours; the backend takes a Go duration.
      ttl: v1.Duration.createFrom({Duration: Math.round(ttlHours * 3600 * 1e9)}),
    })
    CreateSandbox(form).then(b => onCreated(b.name)).catch(e => {
      setError(errText(e))
      setBusy(false)
    })
  }

  return (
    <Dialog title="New sandbox" subtitle="An isolated, resumable agent environment"
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button className="btn btn-primary" onClick={submit} disabled={busy || !f.name || !f.image}>
                {busy ? 'Creating…' : 'Create sandbox'}
              </button>
            </>}>
        <form onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error">{error}</div>}
          {templates.length > 0 && (
            <div className="field">
              <label htmlFor="s-template">Start from template</label>
              <select id="s-template" className="select mono" value={f.template || ''}
                      onChange={e => useTemplate(e.target.value)}>
                <option value="">None</option>
                {templates.map(t => <option key={t.name} value={t.name}>{t.name}</option>)}
              </select>
              <span className="hint">Prefills the image, command, environment and isolation below.</span>
            </div>
          )}

          <div className="field">
            <label htmlFor="s-name">Name</label>
            <input id="s-name" className="input mono" value={f.name}
                   onChange={e => set('name', e.target.value)}
                   placeholder="agent" required/>
            <span className="hint">Lowercase DNS label, at most 63 characters (it names the service too).</span>
          </div>
          <div className="field">
            <label htmlFor="s-image">Image</label>
            <input id="s-image" className="input mono" value={f.image}
                   onChange={e => set('image', e.target.value)}
                   placeholder="cr.example.com:7443/forge-myproj/agent:latest" required/>
            {opts?.images?.length ? <span className="hint">Offered: {opts.images.join(', ')}</span> : null}
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="s-runtime">Isolation</label>
              <select id="s-runtime" className="select mono" value={f.runtimeClass || ''}
                      onChange={e => set('runtimeClass', e.target.value)}>
                <option value="">runc (node runtime)</option>
                {(opts?.runtimeClasses || []).map(r => <option key={r} value={r}>{r}</option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="s-instance">Instance type</label>
              <select id="s-instance" className="select mono" value={f.instanceType || ''}
                      onChange={e => set('instanceType', e.target.value)}>
                <option value="">CPU only</option>
                {types.map(t => <option key={t.name} value={t.name}>
                  {t.name} · {t.gpuCount}×{t.class} · {t.cpu}/{t.memory} ({t.free} free)
                </option>)}
              </select>
            </div>
          </div>
          <div className="field">
            <label htmlFor="s-command">Command (bash)</label>
            <textarea id="s-command" className="textarea" rows={3} value={f.command || ''}
                      onChange={e => set('command', e.target.value)}
                      placeholder="python agent.py"/>
            <span className="hint">Empty runs the image entrypoint. The pod is restarted when it exits.</span>
          </div>
          <div className="field">
            <label htmlFor="s-env">Environment</label>
            <textarea id="s-env" className="textarea" rows={3} value={envText}
                      onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"/>
          </div>
          <div className="field">
            <label htmlFor="s-vols">Volume mounts</label>
            <textarea id="s-vols" className="textarea" rows={2} value={mounts}
                      onChange={e => setMounts(e.target.value)}
                      placeholder={'volume /data [subpath] — one per line'}/>
            <span className="hint">Volumes: {vols.map(v => v.name).join(', ') || 'none in this project'}</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="s-ttl">Auto-stop after (hours)</label>
              <input id="s-ttl" className="input" type="number" min={0} step={0.5} value={ttlHours}
                     onChange={e => setTtlHours(+e.target.value || 0)}/>
              <span className="hint">0 keeps the sandbox until it is stopped{opts?.idleTimeout
                ? ` or idle for ${opts.idleTimeout}` : ''}.</span>
            </div>
            <div className="field">
              <label htmlFor="s-service">Service</label>
              <select id="s-service" className="select" value={f.service ? 'yes' : 'no'}
                      onChange={e => set('service', e.target.value === 'yes')}>
                <option value="yes">Yes (stable DNS name)</option>
                <option value="no">No (pod IP only)</option>
              </select>
            </div>
          </div>
        </form>
    </Dialog>
  )
}

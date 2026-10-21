import {FormEvent, useEffect, useState} from 'react'
import {CreateJob, ListInstanceTypes, TemplateNames, ListVolumes} from '../wailsjs/go/app/App'
import {app, training, volumes} from '../wailsjs/go/models'
import {envToLines, linesToEnv, errText, generateName} from './util'
import {Dialog} from './ui'

// JobCreateDialog is the "New job" form: template prefill, instance type,
// workers, volumes, env and retry defaults.
export function JobCreateDialog({onClose, onCreated}: {
  onClose: () => void, onCreated: (name: string) => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [templates, setTemplates] = useState<string[]>([])
  const [vols, setVols] = useState<volumes.Volume[]>([])
  const [f, setF] = useState(training.Form.createFrom({
    name: generateName('job'), image: '', command: '', workers: 1,
    instanceType: '', env: [], volumes: [],
    maxRetries: 0, retryOn: 'Infra',
  }))
  const [envText, setEnvText] = useState('')
  const [mounts, setMounts] = useState('')

  useEffect(() => {
    ListInstanceTypes().then(ts => {
      setTypes(ts)
      if (ts.length) setF(prev => training.Form.createFrom({...prev, instanceType: ts[0].name}))
    }).catch(e => setError(errText(e)))
    TemplateNames().then(setTemplates).catch(() => {})
    ListVolumes(false).then(vs => {
      setVols(vs)
      if (vs.length) setMounts(`${vs[0].name} /data`)
    }).catch(() => {})
  }, [])

  const set = <K extends keyof training.Form>(k: K, v: training.Form[K]) =>
    setF(prev => training.Form.createFrom({...prev, [k]: v}))

  const useTemplate = (name: string) => {
    set('template', name)
    if (!name) return
    import('../wailsjs/go/app/App').then(({ListTemplates}) =>
      ListTemplates().then(ts => {
        const t = ts.find(t => t.name === name)
        if (t?.form) {
          const nf = training.Form.createFrom({...t.form, name: f.name, template: name})
          setF(nf)
          setEnvText(envToLines(Object.fromEntries((nf.env || []).map(e => [e.name, e.value]))))
          setMounts((nf.volumes || []).map(v => `${v.volume} ${v.mountPath}${v.subPath ? ':' + v.subPath : ''}`).join('\n'))
        }
      }).catch(e => setError(errText(e))))
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    const form = training.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      volumes: mounts.split('\n').map(line => {
        const [volume, path, sub] = line.trim().split(/\s+/)
        if (!volume || !path) return null
        return {volume, mountPath: path, subPath: sub || '', readOnly: false}
      }).filter(Boolean),
    })
    CreateJob(form).then(j => onCreated(j.name)).catch(e => {
      setError(errText(e))
      setBusy(false)
    })
  }

  return (
    <Dialog title="New job" subtitle="Rendered into a TrainJob of the project namespace"
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button type="submit" form="job-form" className="btn btn-primary" disabled={busy || !f.name || !f.image}>
                {busy ? 'Creating…' : 'Create job'}
              </button>
            </>}>
        <form id="job-form" onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error">{error}</div>}
          <div className="field">
            <label htmlFor="j-name">Name</label>
            <input id="j-name" className="input mono" value={f.name}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)}
                   placeholder="lowercase-with-dashes" required/>
            <span className="hint">DNS label; retries are named name-r1, name-r2…</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="j-template">Template</label>
              <select id="j-template" className="select" value={f.template || ''}
                      onChange={e => useTemplate(e.target.value)}>
                <option value="">None (default runtime)</option>
                {templates.map(t => <option key={t} value={t}>{t}</option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="j-instance">Instance type</label>
              <select id="j-instance" className="select mono" value={f.instanceType}
                      onChange={e => set('instanceType', e.target.value)} required>
                {types.map(t => <option key={t.name} value={t.name}>
                  {t.name} · {t.gpuCount}×{t.class} · {t.cpu}/{t.memory} ({t.free} free)
                </option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="j-workers">Workers</label>
              <input id="j-workers" className="input" type="number" min={1} value={f.workers}
                     onChange={e => set('workers', +e.target.value)}/>
            </div>
          </div>
          <div className="field">
            <label htmlFor="j-image">Image</label>
            <input id="j-image" className="input mono" value={f.image}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('image', e.target.value)}
                   placeholder="cr.example.com:7443/forge-myproj/train:latest" required/>
          </div>
          <div className="field">
            <label htmlFor="j-command">Command (bash)</label>
            <textarea id="j-command" className="textarea" rows={4} value={f.command}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => set('command', e.target.value)}
                      placeholder="python train.py --epochs 100"/>
          </div>
          <div className="field">
            <label htmlFor="j-env">Environment</label>
            <textarea id="j-env" className="textarea" rows={3} value={envText}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"/>
          </div>
          <div className="field">
            <label htmlFor="j-vols">Volume mounts</label>
            <textarea id="j-vols" className="textarea" rows={2} value={mounts}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => setMounts(e.target.value)}
                      placeholder={'volume /data [subpath] — one per line'}/>
            <span className="hint">Volumes: {vols.map(v => v.name).join(', ') || 'none in this project'}</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="j-retries">Max retries</label>
              <input id="j-retries" className="input" type="number" min={0} max={20}
                     value={f.maxRetries || 0} onChange={e => set('maxRetries', +e.target.value)}/>
            </div>
            <div className="field">
              <label htmlFor="j-retryon">Retry on</label>
              <select id="j-retryon" className="select" value={f.retryOn || 'Infra'}
                      onChange={e => set('retryOn', e.target.value)}>
                <option value="Infra">Node failures only</option>
                <option value="Any">Any failure</option>
              </select>
            </div>
          </div>
        </form>
    </Dialog>
  )
}

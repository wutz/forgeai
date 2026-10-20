import {useEffect, useState} from 'react'
import {CreateSandboxTemplate, SandboxOptions} from '../wailsjs/go/app/App'
import {app, sandbox} from '../wailsjs/go/models'
import {linesToEnv, fmtDate, errText} from './util'
import {Dialog, Empty} from './ui'

// SandboxTemplateTable lists SandboxTemplates (agent blueprints new sandboxes
// start from) inside the Templates page.
export function SandboxTemplateTable({items, onDelete}: {
  items: app.SandboxTemplate[], onDelete: (name: string) => void
}) {
  if (items.length === 0) {
    return (
      <Empty icon="layers" title="No sandbox templates yet">
        A template captures an image, command and environment so new sandboxes start from it.
      </Empty>
    )
  }
  return (
    <table className="table">
      <thead>
      <tr><th>Name</th><th>Image</th><th>Isolation</th><th>Created</th><th/></tr>
      </thead>
      <tbody>
      {items.map(t => (
        <tr key={t.name}>
          <td className="mono">
            <span className="cell-main">{t.name}</span>
            {t.description && <span className="cell-sub">{t.description}</span>}
          </td>
          <td className="mono trunc" title={t.form?.image}>{t.form?.image || '—'}</td>
          <td className="mono">{t.form?.runtimeClass || 'runc'}</td>
          <td className="muted">{fmtDate(t.created)}</td>
          <td>
            <div className="actions">
              <button className="btn btn-sm btn-danger" onClick={() =>
                confirm(`Delete sandbox template ${t.name}? Existing sandboxes keep running.`) &&
                onDelete(t.name)}>
                Delete
              </button>
            </div>
          </td>
        </tr>
      ))}
      </tbody>
    </table>
  )
}

export function SandboxTemplateDialog({onClose, onSaved}: {onClose: () => void, onSaved: () => void}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [opts, setOpts] = useState<app.SandboxOptions | null>(null)
  const [f, setF] = useState(sandbox.TemplateForm.createFrom({
    name: '', description: '', image: '', command: '', env: [], runtimeClass: '',
  }))
  const [envText, setEnvText] = useState('')

  useEffect(() => {
    SandboxOptions().then(o => {
      setOpts(o)
      setF(prev => sandbox.TemplateForm.createFrom({
        ...prev, image: o.defaultImage, runtimeClass: o.defaultRuntimeClass,
      }))
    }).catch(e => setError(errText(e)))
  }, [])

  const set = <K extends keyof sandbox.TemplateForm>(k: K, v: sandbox.TemplateForm[K]) =>
    setF(prev => sandbox.TemplateForm.createFrom({...prev, [k]: v}))

  const save = () => {
    setBusy(true)
    const payload = sandbox.TemplateForm.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
    })
    CreateSandboxTemplate(payload).then(onSaved).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }

  return (
    <Dialog title="New sandbox template" subtitle="Saved as a SandboxTemplate of the project"
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button className="btn btn-primary" onClick={save} disabled={busy || !f.name || !f.image}>
                {busy ? 'Saving…' : 'Save template'}
              </button>
            </>}>
      <form className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="row">
          <div className="field">
            <label htmlFor="st-name">Name</label>
            <input id="st-name" className="input mono" value={f.name}
                   onChange={e => set('name', e.target.value)} placeholder="agent" required/>
          </div>
          <div className="field">
            <label htmlFor="st-desc">Description</label>
            <input id="st-desc" className="input" value={f.description || ''}
                   onChange={e => set('description', e.target.value)}/>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="st-image">Image</label>
            <input id="st-image" className="input mono" value={f.image}
                   onChange={e => set('image', e.target.value)} required/>
            {opts?.images?.length ? <span className="hint">Offered: {opts.images.join(', ')}</span> : null}
          </div>
          <div className="field">
            <label htmlFor="st-runtime">Isolation</label>
            <select id="st-runtime" className="select mono" value={f.runtimeClass || ''}
                    onChange={e => set('runtimeClass', e.target.value)}>
              <option value="">runc (node runtime)</option>
              {(opts?.runtimeClasses || []).map(r => <option key={r} value={r}>{r}</option>)}
            </select>
          </div>
        </div>
        <div className="field">
          <label htmlFor="st-command">Command (bash)</label>
          <textarea id="st-command" className="textarea" rows={3} value={f.command || ''}
                    onChange={e => set('command', e.target.value)}
                    placeholder="python agent.py"/>
          <span className="hint">Empty runs the image entrypoint.</span>
        </div>
        <div className="field">
          <label htmlFor="st-env">Environment</label>
          <textarea id="st-env" className="textarea" rows={3} value={envText}
                    onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"/>
        </div>
      </form>
    </Dialog>
  )
}

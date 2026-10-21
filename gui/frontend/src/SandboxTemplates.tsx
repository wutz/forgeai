import {useEffect, useState} from 'react'
import {CreateSandboxTemplate, UpdateSandboxTemplate, SandboxOptions} from '../wailsjs/go/app/App'
import {app, sandbox} from '../wailsjs/go/models'
import {envMap, envToLines, linesToEnv, fmtDate, errText, generateName} from './util'
import {Dialog, Empty, useConfirm} from './ui'
import {ImageField} from './ImageField'

// SandboxTemplateTable lists SandboxTemplates (agent blueprints new sandboxes
// start from) inside the Templates page.
export function SandboxTemplateTable({items, onEdit, onDelete}: {
  items: app.SandboxTemplate[], onEdit: (t: app.SandboxTemplate) => void, onDelete: (name: string) => void
}) {
  const confirm = useConfirm()
  if (items.length === 0) {
    return (
      <Empty icon="layers" title="No sandbox templates yet">
        A template captures an image, command and environment so new sandboxes start from it.
      </Empty>
    )
  }
  return (
    <>
    {confirm.dialog}
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
              <button className="btn btn-sm" onClick={() => onEdit(t)}>Edit</button>
              <button className="btn btn-sm btn-danger" onClick={() => confirm.ask({
                title: `Delete sandbox template ${t.name}?`,
                message: 'Existing sandboxes keep running.',
                action: 'Delete template', danger: true,
                run: () => onDelete(t.name),
              })}>
                Delete
              </button>
            </div>
          </td>
        </tr>
      ))}
      </tbody>
    </table>
    </>
  )
}

// SandboxTemplateDialog creates a sandbox template, or edits the existing one
// of that name when existing is set. initial prefills the form (edit, or
// save-as-template from a sandbox); without it the platform defaults apply.
export function SandboxTemplateDialog({initial, existing, onClose, onSaved}: {
  initial?: sandbox.TemplateForm, existing?: string, onClose: () => void, onSaved: (name: string) => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [opts, setOpts] = useState<app.SandboxOptions | null>(null)
  // A new template (also a save-as-template from a sandbox) gets a generated
  // name; an edit keeps its own.
  const [f, setF] = useState(() => {
    const form = initial ?? sandbox.TemplateForm.createFrom({
      name: '', description: '', image: '', command: '', env: [], runtimeClass: '',
    })
    return existing || form.name ? form : sandbox.TemplateForm.createFrom({...form, name: generateName('sandbox-template')})
  })
  const [envText, setEnvText] = useState(initial ? envToLines(envMap(initial.env)) : '')

  useEffect(() => {
    SandboxOptions().then(o => {
      setOpts(o)
      if (initial) return
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
    const req = existing ? UpdateSandboxTemplate(existing, payload) : CreateSandboxTemplate(payload)
    req.then(() => onSaved(payload.name)).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }

  return (
    <Dialog title={`${existing ? 'Edit' : 'New'} sandbox template`}
            subtitle={existing
              ? 'Updates the SandboxTemplate; sandboxes already created from it are not changed'
              : 'Saved as a SandboxTemplate of the project'}
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button type="submit" form="sandbox-template-form" className="btn btn-primary" disabled={busy || !f.name || !f.image}>
                {busy ? 'Saving…' : 'Save template'}
              </button>
            </>}>
      <form id="sandbox-template-form" className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="row">
          <div className="field">
            <label htmlFor="st-name">Name</label>
            <input id="st-name" className="input mono" value={f.name} disabled={!!existing}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} placeholder="agent" required/>
          </div>
          <div className="field">
            <label htmlFor="st-desc">Description</label>
            <input id="st-desc" className="input" value={f.description || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('description', e.target.value)}/>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="st-image">Image</label>
            <ImageField id="st-image" value={f.image} onChange={image => set('image', image)}
                        offered={opts?.images} required/>
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
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => set('command', e.target.value)}
                    placeholder="python agent.py"/>
          <span className="hint">Empty runs the image entrypoint.</span>
        </div>
        <div className="field">
          <label htmlFor="st-env">Environment</label>
          <textarea id="st-env" className="textarea" rows={3} value={envText}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"/>
        </div>
      </form>
    </Dialog>
  )
}

import {useEffect, useState} from 'react'
import {ListTemplates, CreateTemplate, DeleteTemplate} from '../wailsjs/go/app/App'
import {app, training} from '../wailsjs/go/models'
import {envToLines, linesToEnv, fmtDate, errText} from './util'
import {Card, Dialog, Empty, Icon, PageHeader} from './ui'

// Templates lists job templates (namespaced TrainingRuntimes) and edits them.
export function Templates() {
  const [items, setItems] = useState<app.Template[]>([])
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<training.Form | null>(null)

  const refresh = () => ListTemplates().then(items => {
    setItems(items); setError('')
  }).catch(e => setError(errText(e)))
  useEffect(() => { refresh() }, [])

  return (
    <div className="page">
      <PageHeader eyebrow="Workspace" title="Job templates"
                  meta={<span className="muted" style={{fontSize: 12.5}}>{items.length} templates</span>}
                  actions={<button className="btn btn-primary"
                                   onClick={() => setEditing(training.Form.createFrom({
                                     name: '', image: '', command: '', workers: 1, instanceType: '',
                                     env: [], volumes: [], maxRetries: 0, retryOn: 'Infra', description: '',
                                   }))}>
                    <Icon name="plus" size={14}/>New template
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {editing && <TemplateDialog form={editing} onClose={() => setEditing(null)} onSaved={() => {
        setEditing(null); refresh()
      }}/>}

      <Card>
        {items.length === 0 ? (
          <Empty icon="scroll" title="No templates yet">
            A template captures an image, command and defaults so new jobs start from it.
          </Empty>
        ) : (
          <table className="table">
            <thead>
            <tr><th>Name</th><th>Image</th><th>Workers</th><th>Instance</th><th>Created</th><th/></tr>
            </thead>
            <tbody>
            {items.map(t => (
              <tr key={t.name}>
                <td className="mono">
                  <span className="cell-main">{t.name}</span>
                  {t.description && <span className="cell-sub">{t.description}</span>}
                </td>
                <td className="mono trunc" title={t.form?.image}>{t.form?.image || '—'}</td>
                <td>{t.form?.workers || 1}</td>
                <td className="mono">{t.form?.instanceType || '—'}</td>
                <td className="muted">{fmtDate(t.created)}</td>
                <td>
                  <div className="actions">
                    <button className="btn btn-sm" onClick={() =>
                      setEditing(training.Form.createFrom({...t.form, name: t.name}))}>Edit</button>
                    <button className="btn btn-sm btn-danger" onClick={() =>
                      confirm(`Delete template ${t.name}?`) &&
                      DeleteTemplate(t.name).then(refresh).catch(e => setError(errText(e)))}>
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  )
}

function TemplateDialog({form, onClose, onSaved}: {
  form: training.Form, onClose: () => void, onSaved: () => void
}) {
  const [f, setF] = useState(form)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [envText, setEnvText] = useState(envToLines(Object.fromEntries((form.env || []).map(e => [e.name, e.value]))))
  const set = <K extends keyof training.Form>(k: K, v: training.Form[K]) =>
    setF(prev => training.Form.createFrom({...prev, [k]: v}))

  const save = () => {
    setBusy(true)
    const payload = training.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      volumes: [],
    })
    CreateTemplate(payload).then(onSaved).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }

  return (
    <Dialog title={`${form.name ? 'Edit' : 'New'} template`}
            subtitle="Saved as a namespaced TrainingRuntime of the project"
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
            <label htmlFor="t-name">Name</label>
            <input id="t-name" className="input mono" value={f.name}
                   onChange={e => set('name', e.target.value)} required/>
          </div>
          <div className="field">
            <label htmlFor="t-desc">Description</label>
            <input id="t-desc" className="input" value={f.description || ''}
                   onChange={e => set('description', e.target.value)}/>
          </div>
        </div>
        <div className="field">
          <label htmlFor="t-image">Image</label>
          <input id="t-image" className="input mono" value={f.image}
                 onChange={e => set('image', e.target.value)} required/>
        </div>
        <div className="field">
          <label htmlFor="t-command">Command (bash)</label>
          <textarea id="t-command" className="textarea" rows={4} value={f.command || ''}
                    onChange={e => set('command', e.target.value)}/>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="t-instance">Default instance type</label>
            <input id="t-instance" className="input mono" value={f.instanceType}
                   onChange={e => set('instanceType', e.target.value)}
                   placeholder="rtx-5090.x1"/>
          </div>
          <div className="field">
            <label htmlFor="t-workers">Workers</label>
            <input id="t-workers" className="input" type="number" min={1}
                   value={f.workers} onChange={e => set('workers', +e.target.value || 1)}/>
          </div>
        </div>
        <div className="field">
          <label htmlFor="t-env">Environment</label>
          <textarea id="t-env" className="textarea" rows={3} value={envText}
                    onChange={e => setEnvText(e.target.value)}/>
        </div>
      </form>
    </Dialog>
  )
}

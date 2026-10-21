import {useEffect, useState} from 'react'
import {ListTemplates, CreateTemplate, UpdateTemplate, DeleteTemplate, DeleteSandboxTemplate, SandboxTemplates as ListSandboxTemplates, ListInstanceTypes} from '../wailsjs/go/app/App'
import {app, sandbox, training} from '../wailsjs/go/models'
import {envToLines, linesToEnv, fmtDate, errText, generateName} from './util'
import {Card, Dialog, Empty, Icon, PageHeader, Segmented, useConfirm} from './ui'
import {SandboxTemplateDialog, SandboxTemplateTable} from './SandboxTemplates'
import {ImageField} from './ImageField'

type Kind = 'sandbox' | 'job'

const KIND_OPTIONS: {value: Kind, label: string}[] = [
  {value: 'sandbox', label: 'Sandbox'},
  {value: 'job', label: 'Job'},
]

const byName = <T extends {name: string}>(items: T[]) =>
  [...items].sort((a, b) => a.name.localeCompare(b.name))

// Templates holds both kinds in one page: sandbox templates (agent blueprints
// new sandboxes start from) and job templates (namespaced TrainingRuntimes
// that prefill the job form). Each kind is sorted by name.
export function Templates({sandboxes}: {sandboxes: boolean}) {
  const [kind, setKind] = useState<Kind>(sandboxes ? 'sandbox' : 'job')
  const [sandboxItems, setSandboxItems] = useState<app.SandboxTemplate[]>([])
  const [jobItems, setJobItems] = useState<app.Template[]>([])
  const [error, setError] = useState('')
  const [sandboxDialog, setSandboxDialog] = useState<SandboxDialogState | null>(null)
  const [jobDialog, setJobDialog] = useState<JobDialogState | null>(null)

  const refresh = () => Promise.all([
    sandboxes ? ListSandboxTemplates() : Promise.resolve<app.SandboxTemplate[]>([]),
    ListTemplates(),
  ]).then(([s, j]) => {
    setSandboxItems(byName(s)); setJobItems(byName(j)); setError('')
  }).catch(e => setError(errText(e)))
  useEffect(() => { refresh() }, [])

  const onDeleteSandbox = (name: string) =>
    DeleteSandboxTemplate(name).then(refresh).catch(e => setError(errText(e)))
  const onDeleteJob = (name: string) =>
    DeleteTemplate(name).then(refresh).catch(e => setError(errText(e)))

  const isSandbox = sandboxes && kind === 'sandbox'

  return (
    <div className="page">
      <PageHeader eyebrow="Workspace" title="Templates"
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {isSandbox ? sandboxItems.length : jobItems.length} {isSandbox ? 'sandbox' : 'job'} templates
                  </span>}
                  actions={<button className="btn btn-primary" onClick={() => isSandbox
                    ? setSandboxDialog({})
                    : setJobDialog({form: blankJobForm()})}>
                    <Icon name="plus" size={14}/>{isSandbox ? 'New sandbox template' : 'New job template'}
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {sandboxDialog && <SandboxTemplateDialog initial={sandboxDialog.initial} existing={sandboxDialog.existing}
                                               onClose={() => setSandboxDialog(null)} onSaved={() => {
        setSandboxDialog(null); refresh()
      }}/>}
      {jobDialog && <TemplateDialog form={jobDialog.form} existing={jobDialog.existing}
                                    onClose={() => setJobDialog(null)} onSaved={() => {
        setJobDialog(null); refresh()
      }}/>}

      {sandboxes && (
        <div className="list-toolbar">
          <Segmented options={KIND_OPTIONS} value={kind} onChange={setKind}
                     counts={{sandbox: sandboxItems.length, job: jobItems.length}}/>
        </div>
      )}

      <Card>
        {isSandbox
          ? <SandboxTemplateTable items={sandboxItems} onEdit={t => setSandboxDialog({
              initial: sandbox.TemplateForm.createFrom({...t.form, name: t.name}), existing: t.name,
            })} onDelete={onDeleteSandbox}/>
          : <JobTemplateTable items={jobItems} onEdit={t => setJobDialog({
              form: training.Form.createFrom({...t.form, name: t.name}), existing: t.name,
            })} onDelete={onDeleteJob}/>}
      </Card>
    </div>
  )
}

// existing names the template being edited; without it the dialog creates one.
type JobDialogState = {form: training.Form, existing?: string}
type SandboxDialogState = {initial?: sandbox.TemplateForm, existing?: string}

function blankJobForm() {
  return training.Form.createFrom({
    name: '', image: '', command: '', workers: 1, instanceType: '',
    env: [], volumes: [], maxRetries: 0, retryOn: 'Infra', description: '',
  })
}

function JobTemplateTable({items, onEdit, onDelete}: {
  items: app.Template[], onEdit: (t: app.Template) => void, onDelete: (name: string) => void
}) {
  const confirm = useConfirm()
  if (items.length === 0) {
    return (
      <Empty icon="scroll" title="No job templates yet">
        A template captures an image, command and defaults so new jobs start from it.
      </Empty>
    )
  }
  return (
    <>
    {confirm.dialog}
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
              <button className="btn btn-sm" onClick={() => onEdit(t)}>Edit</button>
              <button className="btn btn-sm btn-danger" onClick={() =>
                confirm.ask({
                  title: `Delete template ${t.name}?`,
                  message: 'Jobs already created from it are not changed.',
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

// TemplateDialog creates a job template from form, or edits the existing
// template of that name when existing is set.
export function TemplateDialog({form, existing, onClose, onSaved}: {
  form: training.Form, existing?: string, onClose: () => void, onSaved: (name: string) => void
}) {
  // A new template (also a save-as-template from a job) gets a generated name.
  const [f, setF] = useState(() => existing || form.name
    ? form : training.Form.createFrom({...form, name: generateName('job-template')}))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [envText, setEnvText] = useState(envToLines(Object.fromEntries((form.env || []).map(e => [e.name, e.value]))))
  const set = <K extends keyof training.Form>(k: K, v: training.Form[K]) =>
    setF(prev => training.Form.createFrom({...prev, [k]: v}))

  // The instance type is a choice of the project's types, defaulting to the first.
  useEffect(() => {
    ListInstanceTypes().then(ts => {
      setTypes(ts)
      if (!form.instanceType && ts.length) set('instanceType', ts[0].name)
    }).catch(e => setError(errText(e)))
  }, [])

  const save = () => {
    setBusy(true)
    const payload = training.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      volumes: [],
    })
    const req = existing ? UpdateTemplate(existing, payload) : CreateTemplate(payload)
    req.then(() => onSaved(payload.name ?? '')).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }

  return (
    <Dialog title={`${existing ? 'Edit' : 'New'} template`}
            subtitle={existing
              ? 'Updates the TrainingRuntime; jobs already created from it keep their settings'
              : 'Saved as a namespaced TrainingRuntime of the project'}
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button type="submit" form="template-form" className="btn btn-primary" disabled={busy || !f.name || !f.image}>
                {busy ? 'Saving…' : 'Save template'}
              </button>
            </>}>
      <form id="template-form" className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="row">
          <div className="field">
            <label htmlFor="t-name">Name</label>
            <input id="t-name" className="input mono" value={f.name} disabled={!!existing}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} required/>
          </div>
          <div className="field">
            <label htmlFor="t-desc">Description</label>
            <input id="t-desc" className="input" value={f.description || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('description', e.target.value)}/>
          </div>
        </div>
        <div className="field">
          <label htmlFor="t-image">Image</label>
          <ImageField id="t-image" value={f.image} onChange={image => set('image', image)} required/>
        </div>
        <div className="field">
          <label htmlFor="t-command">Command (bash)</label>
          <textarea id="t-command" className="textarea" rows={4} value={f.command || ''}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => set('command', e.target.value)}/>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="t-instance">Default instance type</label>
            <select id="t-instance" className="select mono" value={f.instanceType}
                    onChange={e => set('instanceType', e.target.value)} required>
              {!f.instanceType && <option value="">Choose a type</option>}
              {f.instanceType && !types.some(t => t.name === f.instanceType) &&
                <option value={f.instanceType}>{f.instanceType} (not available)</option>}
              {types.map(t => <option key={t.name} value={t.name}>{t.name}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="t-workers">Workers</label>
            <input id="t-workers" className="input" type="number" min={1}
                   value={f.workers} onChange={e => set('workers', +e.target.value)}/>
          </div>
        </div>
        <div className="field">
          <label htmlFor="t-env">Environment</label>
          <textarea id="t-env" className="textarea" rows={3} value={envText}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => setEnvText(e.target.value)}/>
        </div>
      </form>
    </Dialog>
  )
}

import {useEffect, useState} from 'react'
import {ListTemplates, CreateTemplate, UpdateTemplate, DeleteTemplate, DeleteSandboxTemplate, SandboxTemplates as ListSandboxTemplates, ListInstanceTypes} from '../wailsjs/go/app/App'
import {app, sandbox, training} from '../wailsjs/go/models'
import {envErrors, envToLines, linesToEnv, fmtDate, errText, generateName, NAME_MAX, NAME_PATTERN} from './util'
import {Card, CancelButton, Dialog, Empty, Icon, PageHeader, Segmented, Spinner, useActive, useConfirm} from './ui'
import {SandboxTemplateDialog, SandboxTemplateTable} from './SandboxTemplates'
import {t} from './i18n'
import {ImageField} from './ImageField'

type Kind = 'sandbox' | 'job'

// kindOptions is built per render so the labels follow the language.
const kindOptions = (): {value: Kind, label: string}[] => [
  {value: 'sandbox', label: t('templates.kindSandbox')},
  {value: 'job', label: t('templates.kindJob')},
]

const byName = <T extends {name: string}>(items: T[]) =>
  [...items].sort((a, b) => a.name.localeCompare(b.name))

// Templates holds both kinds in one page: sandbox templates (agent blueprints
// new sandboxes start from) and job templates (namespaced TrainingRuntimes
// that prefill the job form). Each kind is sorted by name.
export function Templates({sandboxes}: {sandboxes: boolean}) {
  const [kind, setKind] = useState<Kind>(sandboxes ? 'sandbox' : 'job')
  // The lists are null until the first read, so a failed first read is not shown as an empty list.
  const [sandboxItems, setSandboxItems] = useState<app.SandboxTemplate[] | null>(null)
  const [jobItems, setJobItems] = useState<app.Template[] | null>(null)
  const [error, setError] = useState('')
  const [sandboxDialog, setSandboxDialog] = useState<SandboxDialogState | null>(null)
  const [jobDialog, setJobDialog] = useState<JobDialogState | null>(null)
  const active = useActive()

  const refresh = () => Promise.all([
    sandboxes ? ListSandboxTemplates() : Promise.resolve<app.SandboxTemplate[]>([]),
    ListTemplates(),
  ]).then(([s, j]) => {
    setSandboxItems(byName(s)); setJobItems(byName(j)); setError('')
  }).catch(e => setError(errText(e)))
  useEffect(() => { if (active) refresh() }, [active])

  const onDeleteSandbox = (name: string) =>
    DeleteSandboxTemplate(name).then(refresh).catch(e => setError(errText(e)))
  const onDeleteJob = (name: string) =>
    DeleteTemplate(name).then(refresh).catch(e => setError(errText(e)))

  const isSandbox = sandboxes && kind === 'sandbox'

  return (
    <div className="page">
      <PageHeader eyebrow={t('templates.eyebrow')} title={t('templates.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {isSandbox
                      ? t('templates.countSandbox', {count: sandboxItems?.length ?? 0})
                      : t('templates.countJob', {count: jobItems?.length ?? 0})}
                  </span>}
                  actions={<button className="btn" onClick={() => isSandbox
                    ? setSandboxDialog({})
                    : setJobDialog({form: blankJobForm()})}>
                    <Icon name="plus" size={14}/>{isSandbox ? t('templates.newSandbox') : t('templates.newJob')}
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
          <Segmented options={kindOptions()} value={kind} onChange={setKind}
                     counts={{sandbox: sandboxItems?.length ?? 0, job: jobItems?.length ?? 0}}/>
        </div>
      )}

      <Card>
        {isSandbox
          ? <SandboxTemplateTable items={sandboxItems} failed={!!error} onEdit={tpl => setSandboxDialog({
              initial: sandbox.TemplateForm.createFrom({...tpl.form, name: tpl.name}), existing: tpl.name,
            })} onDelete={onDeleteSandbox}/>
          : <JobTemplateTable items={jobItems} failed={!!error} onEdit={tpl => setJobDialog({
              form: training.Form.createFrom({...tpl.form, name: tpl.name}), existing: tpl.name,
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

function JobTemplateTable({items, failed, onEdit, onDelete}: {
  items: app.Template[] | null, failed: boolean, onEdit: (t: app.Template) => void, onDelete: (name: string) => void
}) {
  const confirm = useConfirm()
  if (!items) return failed ? null : <Spinner label={t('common.loading')}/>
  if (items.length === 0) {
    return (
      <Empty icon="scroll" title={t('templates.emptyJobTitle')}>
        {t('templates.emptyJobBody')}
      </Empty>
    )
  }
  return (
    <>
    {confirm.dialog}
    <table className="table">
      <thead>
      <tr><th>{t('common.name')}</th><th>{t('common.image')}</th><th>{t('templates.workers')}</th><th>{t('templates.colInstance')}</th><th>{t('common.created')}</th><th/></tr>
      </thead>
      <tbody>
      {items.map(tpl => (
        <tr key={tpl.name}>
          <td className="mono">
            <span className="cell-main">{tpl.name}</span>
            {tpl.description && <span className="cell-sub">{tpl.description}</span>}
          </td>
          <td className="mono trunc" title={tpl.form?.image}>{tpl.form?.image || '—'}</td>
          <td>{tpl.form?.workers ?? 1}</td>
          <td className="mono">{tpl.form?.instanceType || '—'}</td>
          <td className="muted">{fmtDate(tpl.created)}</td>
          <td>
            <div className="actions">
              <button className="btn btn-sm" onClick={() => onEdit(tpl)}>{t('common.edit')}</button>
              <button className="btn btn-sm btn-danger" onClick={() =>
                confirm.ask({
                  title: t('templates.deleteTitle', {name: tpl.name}),
                  message: t('templates.deleteMessage'),
                  action: t('templates.deleteAction'), danger: true,
                  run: () => onDelete(tpl.name),
                })}>
                {t('common.delete')}
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

  const envBad = envErrors(envText)
  const nameOK = NAME_PATTERN.test(f.name ?? '') && (f.name ?? '').length <= NAME_MAX
  // A template prefills the New job form, which needs at least one worker.
  const workersOK = f.workers >= 1
  // blocker says why Save is off. A bad name, worker count or env line already
  // has a hint under its field, so the footer only names the blockers that have none.
  const blocker = !f.name ? t('common.nameRequired')
    : !nameOK ? t('common.nameInvalid', {max: NAME_MAX})
    : !f.image ? t('common.imageRequired')
    : !f.instanceType ? t('templates.instanceRequired')
    : !workersOK ? t('jobCreate.workersInvalid')
    : envBad.length ? t('common.envInvalid', {lines: envBad.join(', ')}) : ''
  const hasFieldHint = (!nameOK && (f.name ?? '') !== '') || !workersOK || envBad.length > 0

  const save = () => {
    if (blocker) return
    setBusy(true)
    const payload = training.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      // The dialog does not edit mounts; an edited template keeps its own.
      volumes: f.volumes ?? [],
    })
    const req = existing ? UpdateTemplate(existing, payload) : CreateTemplate(payload)
    req.then(() => onSaved(payload.name ?? '')).catch(e => {
      setError(errText(e)); setBusy(false)
    })
  }

  return (
    <Dialog title={existing ? t('templates.dialogEdit') : t('templates.dialogNew')}
            subtitle={existing
              ? t('templates.jobEditSubtitle')
              : t('templates.jobNewSubtitle')}
            onClose={onClose} busy={busy} footer={<>
              {blocker && !busy && !hasFieldHint && <span className="hint hint-error" role="status">{blocker}</span>}
              <CancelButton disabled={busy}/>
              <button type="submit" form="template-form" className={existing ? 'btn btn-primary' : 'btn'} disabled={busy || !!blocker}>
                {busy ? t('templates.saving') : t('templates.saveTemplate')}
              </button>
            </>}>
      <form id="template-form" className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
        {error && <div className="banner banner-error" role="alert">{error}</div>}
        <div className="row">
          <div className="field">
            <label htmlFor="t-name">{t('common.name')}</label>
            <input id="t-name" className="input mono" value={f.name} disabled={!!existing}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false} maxLength={NAME_MAX}
                   aria-invalid={!nameOK}
                   onChange={e => set('name', e.target.value)} required/>
            {!nameOK && (f.name ?? '') !== '' && <span className="hint hint-error">{t('common.nameInvalid', {max: NAME_MAX})}</span>}
          </div>
          <div className="field">
            <label htmlFor="t-desc">{t('templates.description')}</label>
            <input id="t-desc" className="input" value={f.description || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('description', e.target.value)}/>
          </div>
        </div>
        <div className="field">
          <label htmlFor="t-image">{t('common.image')}</label>
          <ImageField id="t-image" value={f.image} onChange={image => set('image', image)} required/>
        </div>
        <div className="field">
          <label htmlFor="t-command">{t('templates.commandBash')}</label>
          <textarea id="t-command" className="textarea" rows={4} value={f.command || ''}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => set('command', e.target.value)}/>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="t-instance">{t('templates.defaultInstanceType')}</label>
            <select id="t-instance" className="select mono" value={f.instanceType}
                    onChange={e => set('instanceType', e.target.value)} required>
              {!f.instanceType && <option value="">{t('templates.chooseType')}</option>}
              {f.instanceType && !types.some(ty => ty.name === f.instanceType) &&
                <option value={f.instanceType}>{t('templates.typeUnavailable', {name: f.instanceType})}</option>}
              {types.map(ty => <option key={ty.name} value={ty.name}>{ty.name}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="t-workers">{t('templates.workers')}</label>
            <input id="t-workers" className="input" type="number" min={1}
                   value={f.workers} onChange={e => set('workers', +e.target.value)}
                   aria-invalid={!workersOK}/>
            {!workersOK && <span className="hint hint-error">{t('jobCreate.workersInvalid')}</span>}
          </div>
        </div>
        <div className="field">
          <label htmlFor="t-env">{t('templates.environment')}</label>
          <textarea id="t-env" className="textarea" rows={3} value={envText}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => setEnvText(e.target.value)}
                    aria-invalid={envBad.length > 0}/>
          {envBad.length > 0 && <span className="hint hint-error">
            {t('common.envInvalid', {lines: envBad.join(', ')})}
          </span>}
        </div>
      </form>
    </Dialog>
  )
}

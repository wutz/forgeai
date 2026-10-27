import {useEffect, useState} from 'react'
import {ListTemplates, CreateTemplate, UpdateTemplate, DeleteTemplate, DeleteSandboxTemplate, SandboxTemplates as ListSandboxTemplates, ListInstanceTypes} from '../wailsjs/go/app/App'
import {app, sandbox, training} from '../wailsjs/go/models'
import {envErrors, envToLines, linesToEnv, everyWhileVisible, fmtDate, errText, generateName, MAX_RETRIES, NAME_MAX, NAME_PATTERN} from './util'
import {Card, CancelButton, Dialog, Empty, HintButton, Icon, PageHeader, RefreshButton, Spinner, useActive, useConfirm, useCreateAccess} from './ui'
import {SandboxTemplateDialog, SandboxTemplateTable} from './SandboxTemplates'
import {t} from './i18n'
import {ImageField} from './ImageField'

type Kind = 'sandbox' | 'job'

const byName = <T extends {name: string}>(items: T[]) =>
  [...items].sort((a, b) => a.name.localeCompare(b.name))

// Templates is one kind's template list, opened from the sidebar under its page:
// sandbox templates (agent blueprints new sandboxes start from) under Sandboxes,
// and job templates (namespaced TrainingRuntimes that prefill the job form) under
// Jobs. Each list is sorted by name.
export function Templates({kind}: {kind: Kind}) {
  // The lists are null until the first read, so a failed first read is not shown as an empty list.
  const [sandboxItems, setSandboxItems] = useState<app.SandboxTemplate[] | null>(null)
  const [jobItems, setJobItems] = useState<app.Template[] | null>(null)
  const [sandboxError, setSandboxError] = useState('')
  const [jobError, setJobError] = useState('')
  const [sandboxDialog, setSandboxDialog] = useState<SandboxDialogState | null>(null)
  const [jobDialog, setJobDialog] = useState<JobDialogState | null>(null)
  const active = useActive()
  const isSandbox = kind === 'sandbox'
  const create = useCreateAccess()
  const canCreate = isSandbox ? create.sandboxTemplates : create.jobTemplates
  const error = isSandbox ? sandboxError : jobError

  // A refused read is shown in the page's banner and rejects, so the header's refresh
  // button reports it too.
  const refreshSandbox = () => ListSandboxTemplates()
    .then(s => { setSandboxItems(byName(s)); setSandboxError('') }, e => { setSandboxError(errText(e)); throw e })
  const refreshJob = () => ListTemplates()
    .then(j => { setJobItems(byName(j)); setJobError('') }, e => { setJobError(errText(e)); throw e })
  // Only the kind on screen is read; the other kind has its own page.
  const refresh = isSandbox ? refreshSandbox : refreshJob
  // reload is for the callers with no place to show a rejection; the failure is already in the page.
  const reload = () => refresh().catch(() => undefined)
  // The lists poll like the other views, so a template saved elsewhere shows without leaving the page.
  useEffect(() => {
    if (!active) return
    reload()
    return everyWhileVisible(reload, 10000)
  }, [active])

  const onDeleteSandbox = (name: string) =>
    DeleteSandboxTemplate(name).then(refreshSandbox).catch(e => setSandboxError(errText(e)))
  const onDeleteJob = (name: string) =>
    DeleteTemplate(name).then(refreshJob).catch(e => setJobError(errText(e)))


  return (
    <div className="page">
      <PageHeader eyebrow={t(isSandbox ? 'sandboxes.title' : 'jobs.title')} title={t(isSandbox ? 'sandboxes.templates' : 'jobs.templates')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {isSandbox
                      ? sandboxItems !== null && t('templates.countSandbox', {count: sandboxItems.length})
                      : jobItems !== null && t('templates.countJob', {count: jobItems.length})}
                  </span>}
                  actions={<>
                    {canCreate && <button className="btn" onClick={() => isSandbox
                      ? setSandboxDialog({})
                      : setJobDialog({form: blankJobForm()})}>
                      <Icon name="plus" size={14}/>{isSandbox ? t('templates.newSandbox') : t('templates.newJob')}
                    </button>}
                    <RefreshButton onRefresh={refresh}/>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">
        <span>{error}</span>
        {/* A conflict says to reload; the refresh is here so the user does not have to find it. */}
        <span className="spacer"/>
        <button className="btn btn-sm" onClick={() => reload()}>{t('common.refresh')}</button>
      </div>}
      {/* Closing a dialog reloads the list: a save that failed on a stale version
          says to reload, and the reload here gives the reopened edit the current version. */}
      {sandboxDialog &&<SandboxTemplateDialog initial={sandboxDialog.initial} existing={sandboxDialog.existing} version={sandboxDialog.version}
                                               onClose={() => { setSandboxDialog(null); reload() }} onSaved={() => {
        setSandboxDialog(null); reload()
      }}/>}
      {jobDialog && <TemplateDialog form={jobDialog.form} existing={jobDialog.existing} version={jobDialog.version}
                                    onClose={() => { setJobDialog(null); reload() }} onSaved={() => {
        setJobDialog(null); reload()
      }}/>}

      {/* A failed first read leaves no list, so the card is left out rather than drawn empty under the banner. */}
      {((isSandbox ? sandboxItems : jobItems) || !error) && <Card>
        {isSandbox
          ? <SandboxTemplateTable items={sandboxItems} failed={!!error} onEdit={tpl => setSandboxDialog({
              initial: sandbox.TemplateForm.createFrom({...tpl.form, name: tpl.name}), existing: tpl.name, version: tpl.resourceVersion,
            })} onDelete={onDeleteSandbox}/>
          : <JobTemplateTable items={jobItems} failed={!!error} onEdit={tpl => setJobDialog({
              form: training.Form.createFrom({...tpl.form, name: tpl.name}), existing: tpl.name, version: tpl.resourceVersion,
            })} onDelete={onDeleteJob}/>}
      </Card>}
    </div>
  )
}

// existing names the template being edited; without it the dialog creates one.
type JobDialogState = {form: training.Form, existing?: string, version?: string}
type SandboxDialogState = {initial?: sandbox.TemplateForm, existing?: string, version?: string}

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
  // The instance types say which templates name a type the project no longer offers. A failed
  // read marks nothing, since the list cannot tell an unavailable type from an unread one.
  const [offered, setOffered] = useState<Set<string> | null>(null)
  useEffect(() => {
    ListInstanceTypes().then(ts => setOffered(new Set(ts.map(ty => ty.name)))).catch(() => {})
  }, [])
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
      <tr><th>{t('common.name')}</th><th>{t('common.image')}</th><th>{t('templates.workers')}</th><th>{t('templates.colInstance')}</th><th className="col-optional">{t('common.created')}</th><th/></tr>
      </thead>
      <tbody>
      {items.map(tpl => (
        <tr key={tpl.name}>
          <td data-label={t('common.name')} className="mono">
            <span className="cell-main">{tpl.name}</span>
            {tpl.description && <span className="cell-sub">{tpl.description}</span>}
            {tpl.error && <span className="cell-sub" role="status">{t('templates.unreadable')}</span>}
          </td>
          <td data-label={t('common.image')} className="mono trunc" title={tpl.form?.image}>{tpl.form?.image || '—'}</td>
          <td data-label={t('templates.workers')}>{tpl.error ? '—' : (tpl.form?.workers ?? 1)}</td>
          <td data-label={t('templates.colInstance')} className="mono">{tpl.form?.instanceType
            ? offered && !offered.has(tpl.form.instanceType)
              ? <span className="muted">{t('templates.typeUnavailable', {name: tpl.form.instanceType})}</span>
              : tpl.form.instanceType
            : '—'}</td>
          <td data-label={t('common.created')} className="muted col-optional">{fmtDate(tpl.created)}</td>
          <td>
            <div className="actions">
              <HintButton className="btn btn-sm" disabled={!!tpl.error} reason={t('templates.unreadableHint')}
                          onClick={() => onEdit(tpl)}>{t('common.edit')}</HintButton>
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
export function TemplateDialog({form, existing, version, onClose, onSaved}: {
  form: training.Form, existing?: string, version?: string, onClose: () => void, onSaved: (name: string) => void
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
  const workersOK = Number.isInteger(f.workers) && f.workers >= 1
  // The platform accepts 0 to MAX_RETRIES retries; the backend rejects anything else.
  const retries = f.maxRetries ?? 0
  const retriesOK = Number.isInteger(retries) && retries >= 0 && retries <= MAX_RETRIES
  // blocker says why Save is off. A bad name, worker count or env line already
  // has a hint under its field, so the footer only names the blockers that have none.
  const blocker = !f.name ? t('common.nameRequired')
    : !nameOK ? t('common.nameInvalid', {max: NAME_MAX})
    : !f.image ? t('common.imageRequired')
    : !f.instanceType ? t('templates.instanceRequired')
    : !workersOK ? t('jobCreate.workersInvalid')
    : !retriesOK ? t('jobCreate.retriesInvalid')
    // A type the project no longer offers would save a template whose jobs are refused; the
    // select already names it, so the footer asks for a type the project offers.
    : types.length > 0 && !types.some(ty => ty.name === f.instanceType) ? t('templates.instanceRequired')
    : envBad.length ? t('common.envInvalid', {lines: envBad.join(', ')}) : ''
  const hasFieldHint = (!nameOK && (f.name ?? '') !== '') || !workersOK || !retriesOK || envBad.length > 0

  const save = () => {
    if (blocker) return
    setBusy(true)
    const payload = training.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      // The dialog does not edit mounts; an edited template keeps its own.
      volumes: f.volumes ?? [],
    })
    const req = existing ? UpdateTemplate(existing, version ?? '', payload) : CreateTemplate(payload)
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
        {!!f.volumes?.length && (
          <div className="field" role="group" aria-labelledby="t-mounts-label">
            <label id="t-mounts-label">{t('jobCreate.volumeMounts')}</label>
            {f.volumes.map((m, i) => (
              <div key={i} className="mono" style={{fontSize: 13}}>
                {m.volume} → {m.mountPath}{m.subPath ? ` (${m.subPath})` : ''}{m.readOnly ? ` · ${t('mounts.readOnly')}` : ''}
              </div>
            ))}
            <span className="hint">{t('templates.mountsKept')}</span>
          </div>
        )}
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
                   value={Number.isNaN(f.workers) ? '' : f.workers}
                   onChange={e => set('workers', e.target.value === '' ? NaN : +e.target.value)}
                   aria-invalid={!workersOK}/>
            {!workersOK && <span className="hint hint-error">{t('jobCreate.workersInvalid')}</span>}
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="t-retries">{t('jobCreate.maxRetries')}</label>
            <input id="t-retries" className="input" type="number" min={0} max={MAX_RETRIES}
                   value={Number.isNaN(f.maxRetries) ? '' : f.maxRetries ?? 0} aria-invalid={!retriesOK}
                   onChange={e => set('maxRetries', e.target.value === '' ? NaN : +e.target.value)}/>
            {!retriesOK && <span className="hint hint-error">{t('jobCreate.retriesInvalid')}</span>}
          </div>
          <div className="field">
            <label htmlFor="t-retryon">{t('jobCreate.retryOn')}</label>
            <select id="t-retryon" className="select" value={f.retryOn || 'Infra'}
                    onChange={e => set('retryOn', e.target.value)}>
              <option value="Infra">{t('jobCreate.retryInfraOnly')}</option>
              <option value="Any">{t('jobCreate.retryAnyFailure')}</option>
            </select>
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

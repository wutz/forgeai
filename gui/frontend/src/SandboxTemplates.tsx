import {useEffect, useState} from 'react'
import {CreateSandboxTemplate, UpdateSandboxTemplate, SandboxOptions} from '../wailsjs/go/app/App'
import {app, sandbox} from '../wailsjs/go/models'
import {envErrors, envMap, envToLines, linesToEnv, fmtDate, errText, generateName, NAME_MAX, NAME_PATTERN} from './util'
import {Dialog, Empty, useConfirm} from './ui'
import {t} from './i18n'
import {ImageField} from './ImageField'

// SandboxTemplateTable lists SandboxTemplates (agent blueprints new sandboxes
// start from) inside the Templates page.
export function SandboxTemplateTable({items, onEdit, onDelete}: {
  items: app.SandboxTemplate[], onEdit: (t: app.SandboxTemplate) => void, onDelete: (name: string) => void
}) {
  const confirm = useConfirm()
  if (items.length === 0) {
    return (
      <Empty icon="layers" title={t('sandboxTemplates.emptyTitle')}>
        {t('sandboxTemplates.emptyBody')}
      </Empty>
    )
  }
  return (
    <>
    {confirm.dialog}
    <table className="table">
      <thead>
      <tr><th>{t('common.name')}</th><th>{t('common.image')}</th><th>{t('sandboxTemplates.colIsolation')}</th><th>{t('common.created')}</th><th/></tr>
      </thead>
      <tbody>
      {items.map(tpl => (
        <tr key={tpl.name}>
          <td className="mono">
            <span className="cell-main">{tpl.name}</span>
            {tpl.description && <span className="cell-sub">{tpl.description}</span>}
          </td>
          <td className="mono trunc" title={tpl.form?.image}>{tpl.form?.image || '—'}</td>
          <td className="mono">{tpl.form?.runtimeClass || 'runc'}</td>
          <td className="muted">{fmtDate(tpl.created)}</td>
          <td>
            <div className="actions">
              <button className="btn btn-sm" onClick={() => onEdit(tpl)}>{t('common.edit')}</button>
              <button className="btn btn-sm btn-danger" onClick={() => confirm.ask({
                title: t('sandboxTemplates.deleteTitle', {name: tpl.name}),
                message: t('sandboxTemplates.deleteMessage'),
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

  const envBad = envErrors(envText)
  // An edit keeps its name (the field is disabled), so only a new name is checked.
  const nameOK = !!existing || (NAME_PATTERN.test(f.name ?? '') && (f.name ?? '').length <= NAME_MAX)
  // blocker says why Save is off. A bad name or env line already has a hint
  // under its field, so the footer only names the blockers that have none.
  const blocker = !f.name ? t('common.nameRequired')
    : !nameOK ? t('common.nameInvalid')
    : !f.image ? t('common.imageRequired')
    : envBad.length ? t('common.envInvalid', {lines: envBad.join(', ')}) : ''
  const hasFieldHint = (!nameOK && (f.name ?? '') !== '') || envBad.length > 0

  const save = () => {
    if (blocker) return
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
    <Dialog title={existing ? t('sandboxTemplates.dialogEdit') : t('sandboxTemplates.dialogNew')}
            subtitle={existing
              ? t('sandboxTemplates.editSubtitle')
              : t('sandboxTemplates.newSubtitle')}
            onClose={onClose} busy={busy} footer={<>
              {blocker && !busy && !hasFieldHint && <span className="hint hint-error" role="status">{blocker}</span>}
              <button className="btn" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
              <button type="submit" form="sandbox-template-form" className="btn btn-primary" disabled={busy || !!blocker}>
                {busy ? t('templates.saving') : t('templates.saveTemplate')}
              </button>
            </>}>
      <form id="sandbox-template-form" className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="row">
          <div className="field">
            <label htmlFor="st-name">{t('common.name')}</label>
            <input id="st-name" className="input mono" value={f.name} disabled={!!existing}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} placeholder="agent" required
                   maxLength={existing ? undefined : NAME_MAX} aria-invalid={!nameOK}/>
            {!nameOK && (f.name ?? '') !== '' && <span className="hint hint-error">{t('common.nameInvalid')}</span>}
          </div>
          <div className="field">
            <label htmlFor="st-desc">{t('templates.description')}</label>
            <input id="st-desc" className="input" value={f.description || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('description', e.target.value)}/>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="st-image">{t('common.image')}</label>
            <ImageField id="st-image" value={f.image} onChange={image => set('image', image)}
                        offered={opts?.images} required/>
          </div>
          <div className="field">
            <label htmlFor="st-runtime">{t('sandboxTemplates.colIsolation')}</label>
            <select id="st-runtime" className="select mono" value={f.runtimeClass || ''}
                    onChange={e => set('runtimeClass', e.target.value)}>
              <option value="">{t('sandboxTemplates.runcNodeRuntime')}</option>
              {(opts?.runtimeClasses || []).map(r => <option key={r} value={r}>{r}</option>)}
            </select>
          </div>
        </div>
        <div className="field">
          <label htmlFor="st-command">{t('templates.commandBash')}</label>
          <textarea id="st-command" className="textarea" rows={3} value={f.command || ''}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => set('command', e.target.value)}
                    placeholder="python agent.py"/>
          <span className="hint">{t('sandboxTemplates.commandHint')}</span>
        </div>
        <div className="field">
          <label htmlFor="st-env">{t('templates.environment')}</label>
          <textarea id="st-env" className="textarea" rows={3} value={envText}
                    autoCapitalize="none" autoCorrect="off" spellCheck={false}
                    onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"
                    aria-invalid={envBad.length > 0}/>
          {envBad.length > 0 && <span className="hint hint-error">
            {t('common.envInvalid', {lines: envBad.join(', ')})}
          </span>}
        </div>
      </form>
    </Dialog>
  )
}

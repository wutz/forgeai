import {FormEvent, useEffect, useState} from 'react'
import {CreateSandbox, ListInstanceTypes, ListVolumes, SandboxOptions, SandboxTemplates} from '../wailsjs/go/app/App'
import {app, sandbox, volumes, v1} from '../wailsjs/go/models'
import {envToLines, linesToEnv, errText, generateName} from './util'
import {Dialog} from './ui'
import {t} from './i18n'
import {Mount, VolumeMountsField, mountProblems} from './VolumeMounts'

// SandboxCreateDialog is the "New sandbox" form: image, isolation runtime
// class, optional instance type and volumes, and the auto-stop timer.
export function SandboxCreateDialog({onClose, onCreated}: {
  onClose: () => void, onCreated: (name: string) => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [opts, setOpts] = useState<app.SandboxOptions | null>(null)
  const [types, setTypes] = useState<app.InstanceType[]>([])
  // vols is null until the project's volumes load.
  const [vols, setVols] = useState<volumes.Volume[] | null>(null)
  const [f, setF] = useState<sandbox.Form>(sandbox.Form.createFrom({
    name: generateName('sandbox'), image: '', command: '', env: [], instanceType: '', runtimeClass: '', service: true,
    volumes: [], ttl: v1.Duration.createFrom({Duration: 0}),
  }))
  const [envText, setEnvText] = useState('')
  const [mounts, setMounts] = useState<Mount[]>([])
  const [ttlHours, setTtlHours] = useState(0)
  // idleMinutes stays null until typed: null uses the platform's idle timeout,
  // 0 turns the idle policy off for this sandbox.
  const [idleMinutes, setIdleMinutes] = useState<number | null>(null)
  const [templates, setTemplates] = useState<app.SandboxTemplate[]>([])

  useEffect(() => {
    SandboxOptions().then(o => {
      setOpts(o)
      setF(prev => sandbox.Form.createFrom({
        ...prev, image: o.defaultImage, runtimeClass: o.defaultRuntimeClass,
      }))
    }).catch(e => setError(errText(e)))
    ListInstanceTypes().then(setTypes).catch(() => {})
    ListVolumes(false).then(setVols).catch(e => setError(errText(e)))
    SandboxTemplates().then(setTemplates).catch(() => {})
  }, [])

  const set = <K extends keyof sandbox.Form>(k: K, v: sandbox.Form[K]) =>
    setF(prev => sandbox.Form.createFrom({...prev, [k]: v}))

  // useTemplate prefills the agent fields from a SandboxTemplate; the fields
  // stay editable afterwards.
  const useTemplate = (name: string) => {
    const form = templates.find(tpl => tpl.name === name)?.form
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

  // Mounts need the project's volumes to check against; without any mounts the
  // form does not wait for them. A sandbox runs on one node.
  const mountsOK = mounts.length === 0 ||
    (vols !== null && mountProblems(mounts, vols, 1).every(p => !p))

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!mountsOK) return
    setBusy(true)
    const form = sandbox.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      volumes: mounts.map(m => ({
        volume: m.volume.trim(), mountPath: m.mountPath.trim(), subPath: (m.subPath || '').trim(),
        readOnly: !!m.readOnly,
      })),
      // The form collects hours; the backend takes a Go duration.
      ttl: v1.Duration.createFrom({Duration: Math.round(ttlHours * 3600 * 1e9)}),
      idleTimeout: idleMinutes === null
        ? undefined
        : v1.Duration.createFrom({Duration: Math.round(idleMinutes * 60 * 1e9)}),
    })
    CreateSandbox(form).then(b => onCreated(b.name)).catch(e => {
      setError(errText(e))
      setBusy(false)
    })
  }

  return (
    <Dialog title={t('sandboxCreate.title')} subtitle={t('sandboxCreate.subtitle')}
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>{t('common.cancel')}</button>
              <button type="submit" form="sandbox-form" className="btn btn-primary" disabled={busy || !f.name || !f.image || !mountsOK}>
                {busy ? t('sandboxCreate.creating') : t('sandboxCreate.create')}
              </button>
            </>}>
        <form id="sandbox-form" onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error">{error}</div>}
          {templates.length > 0 && (
            <div className="field">
              <label htmlFor="s-template">{t('sandboxCreate.fromTemplate')}</label>
              <select id="s-template" className="select mono" value={f.template || ''}
                      onChange={e => useTemplate(e.target.value)}>
                <option value="">{t('common.none')}</option>
                {templates.map(tpl => <option key={tpl.name} value={tpl.name}>{tpl.name}</option>)}
              </select>
              <span className="hint">{t('sandboxCreate.templateHint')}</span>
            </div>
          )}

          <div className="field">
            <label htmlFor="s-name">{t('common.name')}</label>
            <input id="s-name" className="input mono" value={f.name}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)}
                   placeholder="agent" required/>
            <span className="hint">{t('sandboxCreate.nameHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="s-image">{t('common.image')}</label>
            <input id="s-image" className="input mono" value={f.image}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('image', e.target.value)}
                   placeholder="cr.example.com:7443/forge-myproj/agent:latest" required/>
            {opts?.images?.length ? <span className="hint">{t('sandboxCreate.offered', {images: opts.images.join(', ')})}</span> : null}
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="s-runtime">{t('sandboxCreate.isolation')}</label>
              <select id="s-runtime" className="select mono" value={f.runtimeClass || ''}
                      onChange={e => set('runtimeClass', e.target.value)}>
                <option value="">{t('sandboxCreate.runcOption')}</option>
                {(opts?.runtimeClasses || []).map(r => <option key={r} value={r}>{r}</option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="s-instance">{t('sandboxCreate.instanceType')}</label>
              <select id="s-instance" className="select mono" value={f.instanceType || ''}
                      onChange={e => set('instanceType', e.target.value)}>
                <option value="">{t('sandboxCreate.cpuOnly')}</option>
                {types.map(ty => <option key={ty.name} value={ty.name}>
                  {ty.name} · {ty.gpuCount}×{ty.class} · {ty.cpu}/{ty.memory} {t('sandboxCreate.instanceFree', {free: ty.free})}
                </option>)}
              </select>
            </div>
          </div>
          <div className="field">
            <label htmlFor="s-command">{t('sandboxCreate.command')}</label>
            <textarea id="s-command" className="textarea" rows={3} value={f.command || ''}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => set('command', e.target.value)}
                      placeholder="python agent.py"/>
            <span className="hint">{t('sandboxCreate.commandHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="s-env">{t('sandboxCreate.environment')}</label>
            <textarea id="s-env" className="textarea" rows={3} value={envText}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"/>
          </div>
          <div className="field">
            <label>{t('sandboxCreate.volumeMounts')}</label>
            <VolumeMountsField mounts={mounts} onChange={setMounts} vols={vols}/>
            <span className="hint">{t('mounts.sandboxHint')}</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="s-ttl">{t('sandboxCreate.ttl')}</label>
              <input id="s-ttl" className="input" type="number" min={0} step={0.5} value={ttlHours}
                     onChange={e => setTtlHours(+e.target.value)}/>
              <span className="hint">{t('sandboxCreate.ttlHint')}</span>
            </div>
            <div className="field">
              <label htmlFor="s-idle">{t('sandboxCreate.idle')}</label>
              <input id="s-idle" className="input" type="number" min={0} step={1} value={idleMinutes ?? ''}
                     onChange={e => setIdleMinutes(e.target.value === '' ? null : +e.target.value)}
                     placeholder={opts?.idleTimeout
                       ? t('sandboxCreate.idleDefault', {value: opts.idleTimeout})
                       : t('sandboxCreate.off')}/>
              <span className="hint">{t('sandboxCreate.idleHint', {timeout: opts?.idleTimeout || t('sandboxCreate.off')})}</span>
            </div>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="s-service">{t('sandboxCreate.serviceLabel')}</label>
              <select id="s-service" className="select" value={f.service ? 'yes' : 'no'}
                      onChange={e => set('service', e.target.value === 'yes')}>
                <option value="yes">{t('sandboxCreate.serviceYes')}</option>
                <option value="no">{t('sandboxCreate.serviceNo')}</option>
              </select>
            </div>
          </div>
        </form>
    </Dialog>
  )
}

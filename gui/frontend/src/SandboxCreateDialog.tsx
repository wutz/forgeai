import {FormEvent, useEffect, useRef, useState} from 'react'
import {CatalogSandboxForm, CatalogSources, Config, CreateSandbox, ListInstanceTypes, ListVolumes, SandboxOptions, SandboxTemplates} from '../wailsjs/go/app/App'
import {app, sandbox, volumes, v1} from '../wailsjs/go/models'
import {envErrors, envToLines, formatGoDuration, linesToEnv, errText, generateName, NAME_PATTERN, SANDBOX_NAME_MAX, SSH_NAME_MAX} from './util'
import {CancelButton, Dialog} from './ui'
import {t} from './i18n'
import {ImageField} from './ImageField'
import {Mount, VolumeMountsField, mountProblems} from './VolumeMounts'
import {catalogGroups, catalogPick, loadTemplateSources, parseCatalogPick, pickedCatalogTemplate} from './templateSources'

// MAX_TTL_HOURS caps the auto-stop and idle timeouts at one year; the backend refuses more.
const MAX_TTL_HOURS = 24 * 365

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
  const [volsError, setVolsError] = useState('')
  const [f, setF] = useState<sandbox.Form>(sandbox.Form.createFrom({
    name: generateName('sandbox'), image: '', command: '', env: [], instanceType: '', runtimeClass: '', service: true, ssh: false,
    volumes: [], ttl: v1.Duration.createFrom({Duration: 0}),
  }))
  const [envText, setEnvText] = useState('')
  const [mounts, setMounts] = useState<Mount[]>([])
  // NaN is an empty field, which is not a valid TTL (an empty field would read as 0 hours).
  const [ttlHours, setTtlHours] = useState(0)
  // idleMinutes stays null until typed: null uses the platform's idle timeout,
  // 0 turns the idle policy off for this sandbox.
  const [idleMinutes, setIdleMinutes] = useState<number | null>(null)
  // gatewayText is the gateway port field: empty leaves the sandbox inside the cluster.
  const [gatewayText, setGatewayText] = useState('')
  // gatewayOn is false when the platform has no gateway to publish through; until the
  // config loads, the field stays open and the backend has the last word.
  const [gatewayOn, setGatewayOn] = useState(true)
  const [templates, setTemplates] = useState<app.SandboxTemplate[]>([])
  // A failed read says so; an empty picker would read as a cluster with no templates.
  const [templatesError, setTemplatesError] = useState('')
  // sources holds the template sources: the platform's, and the ones the user added.
  const [sources, setSources] = useState<app.CatalogSource[]>([])
  const [catalogError, setCatalogError] = useState('')
  // pick is the template select's value: "" for none, "project:<name>", or a catalogPick value.
  const [pick, setPick] = useState('')

  useEffect(() => {
    SandboxOptions().then(o => {
      setOpts(o)
      // A template or image the user already picked is kept; the defaults fill only blanks.
      setF(prev => sandbox.Form.createFrom({
        ...prev, image: prev.image || o.defaultImage, runtimeClass: prev.runtimeClass || o.defaultRuntimeClass,
      }))
    }).catch(e => setError(errText(e)))
    ListInstanceTypes().then(setTypes).catch(e => setError(errText(e)))
    ListVolumes(false).then(setVols).catch(e => setVolsError(errText(e)))
    Config().then(c => setGatewayOn(!!c.gateway?.name), () => {})
    SandboxTemplates().then(ts => { setTemplates(ts); setTemplatesError('') }, e => setTemplatesError(errText(e)))
    CatalogSources(loadTemplateSources()).then(s => {
      setSources(s)
      setCatalogError('')
    }, e => setCatalogError(errText(e)))
  }, [])

  const set = <K extends keyof sandbox.Form>(k: K, v: sandbox.Form[K]) =>
    setF(prev => sandbox.Form.createFrom({...prev, [k]: v}))

  // pickSeq numbers the template choices: a reply for an earlier choice is
  // dropped, so a slow load cannot overwrite the template chosen after it.
  const pickSeq = useRef(0)
  // useTemplate prefills the agent fields from a project or catalog
  // SandboxTemplate; the fields stay editable afterwards.
  const useTemplate = (value: string) => {
    const seq = ++pickSeq.current
    setPick(value)
    const sep = value.indexOf(':')
    if (sep < 0) {
      set('template', '')
      return
    }
    const kind = value.slice(0, sep)
    const name = value.slice(sep + 1)
    // Only a project template is referenced by name; a catalog template is a prefill.
    const projectRef = kind === 'project' ? name : ''
    set('template', projectRef)
    let load: Promise<sandbox.TemplateForm | undefined>
    if (kind === 'catalog') {
      const [source, template] = parseCatalogPick(value)
      load = CatalogSandboxForm(source, template)
    } else {
      load = Promise.resolve(templates.find(tpl => tpl.name === name)?.form)
    }
    load.then(form => {
      if (seq !== pickSeq.current || !form) return
      setF(prev => sandbox.Form.createFrom({
        ...prev,
        template: projectRef,
        image: form.image,
        command: form.command || '',
        runtimeClass: form.runtimeClass || '',
      }))
      setEnvText(envToLines(Object.fromEntries((form.env || []).map(e => [e.name, e.value]))))
    }).catch(e => {
      if (seq === pickSeq.current) setError(errText(e))
    })
  }
  const groups = catalogGroups(sources, 'Sandbox')
  const picked = pickedCatalogTemplate(groups, pick)

  // Mounts need the project's volumes to check against; without any mounts the
  // form does not wait for them. A sandbox runs on one node.
  const mountsOK = mounts.length === 0 ||
    (vols !== null && mountProblems(mounts, vols, 1).every(p => !p))

  const envBad = envErrors(envText)
  // The backend rejects a name that does not start with a letter, or is longer
  // than the sandbox limit (shorter with SSH); the field says so before Create is pressed.
  const nameMax = f.ssh ? SSH_NAME_MAX : SANDBOX_NAME_MAX
  const nameOK = NAME_PATTERN.test(f.name ?? '') && (f.name ?? '').length <= nameMax
  // Negative durations are refused by the backend; the fields say so before Create.
  const ttlOK = Number.isFinite(ttlHours) && ttlHours >= 0 && ttlHours <= MAX_TTL_HOURS
  const idleOK = idleMinutes === null || (Number.isFinite(idleMinutes) && idleMinutes >= 0 && idleMinutes <= MAX_TTL_HOURS * 60)
  // The gateway port is optional; empty is 0, which the backend takes as not published.
  const gatewayPort = gatewayText.trim() === '' ? 0 : Number(gatewayText)
  const gatewayOK = gatewayText.trim() === '' || (Number.isInteger(gatewayPort) && gatewayPort >= 1 && gatewayPort <= 65535)
  // The footer names the blocker that has no field hint of its own, so a disabled
  // Create is never unexplained.
  const blocker = !f.name ? t('common.nameRequired')
    : !f.image.trim() ? t('common.imageRequired')
    : !mountsOK ? (vols === null ? (volsError ? t('mounts.loadFailed') : t('mounts.loading')) : t('mounts.fixFirst'))
    : ''

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!nameOK || !mountsOK || envBad.length || !ttlOK || !idleOK || !gatewayOK) return
    setBusy(true)
    const form = sandbox.Form.createFrom({
      ...f,
      image: f.image.trim(),
      gatewayPort,
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
            onClose={onClose} busy={busy} footer={<>
              {blocker && !busy && <span className="hint hint-error" role="status">{blocker}</span>}
              <CancelButton disabled={busy}/>
              <button type="submit" form="sandbox-form" className="btn" disabled={busy || !nameOK || !f.image.trim() || !mountsOK || envBad.length > 0 || !ttlOK || !idleOK || !gatewayOK}>
                {busy ? t('sandboxCreate.creating') : t('sandboxCreate.create')}
              </button>
            </>}>
        <form id="sandbox-form" onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error" role="alert">{error}</div>}
          {templatesError && <div className="banner banner-warn" role="status">{t('sandboxCreate.templatesUnavailable', {error: templatesError})}</div>}
          {catalogError && <div className="banner banner-warn" role="status">{t('sandboxCreate.catalogUnavailable', {error: catalogError})}</div>}
          {sources.filter(s => s.error).map(s => <div key={s.location} className="banner banner-warn" role="status">
            {t('sandboxCreate.catalogUnavailable', {error: `${s.platform ? '' : `${s.location}: `}${s.error}`})}
          </div>)}
          {(templates.length > 0 || groups.length > 0) && (
            <div className="field">
              <label htmlFor="s-template">{t('sandboxCreate.fromTemplate')}</label>
              <select id="s-template" className="select mono" value={pick}
                      onChange={e => useTemplate(e.target.value)}>
                <option value="">{t('common.none')}</option>
                {templates.some(tpl => !tpl.error) && <optgroup label={t('sandboxCreate.projectTemplates')}>
                  {templates.filter(tpl => !tpl.error).map(tpl => <option key={tpl.name} value={`project:${tpl.name}`}>{tpl.name}</option>)}
                </optgroup>}
                {groups.map(g => <optgroup key={g.source.location}
                                          label={g.source.platform ? t('sandboxCreate.catalogTemplates') : g.source.displayName || g.source.location}>
                  {g.templates.map(c => <option key={catalogPick(g.source.location, c.name)} value={catalogPick(g.source.location, c.name)}>
                    {c.displayName}
                  </option>)}
                </optgroup>)}
              </select>
              {picked?.template.description && <span className="hint">{picked.template.description}</span>}
              {picked && !picked.source.platform && <div className="banner banner-warn" role="status">
                {t('templateSources.untrusted', {source: picked.source.location})}
              </div>}
              <span className="hint">{t('sandboxCreate.templateHint')}</span>
              <span className="hint">{t('templateSources.manageHint')}</span>
            </div>
          )}

          <div className="field">
            <label htmlFor="s-name">{t('common.name')}</label>
            <input id="s-name" className="input mono" value={f.name}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} maxLength={nameMax}
                   aria-invalid={!nameOK}
                   placeholder="agent" required/>
            <span className="hint">{t('sandboxCreate.nameHint')}</span>
            {!nameOK && (f.name ?? '') !== '' && <span className="hint hint-error">
              {t('common.nameInvalid', {max: nameMax})}
            </span>}
          </div>
          <div className="field">
            <label htmlFor="s-image">{t('common.image')}</label>
            <ImageField id="s-image" value={f.image} onChange={image => set('image', image)}
                        offered={opts?.images} example="agent:latest" required/>
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
                  {/* Only a GPU class has a status; a type without one reports no free count. */}
                  {ty.name} · {ty.gpuCount ? `${ty.gpuCount}×${ty.class}` : t('sandboxCreate.cpuOnly')} · {ty.cpu}/{ty.memory}{ty.total > 0 && t('sandboxCreate.instanceFree', {free: ty.free})}
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
                      onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"
                      aria-invalid={envBad.length > 0}/>
            {envBad.length > 0 && <span className="hint hint-error">
              {t('common.envInvalid', {lines: envBad.join(', ')})}
            </span>}
          </div>
          <div className="field">
            <label>{t('sandboxCreate.volumeMounts')}</label>
            <VolumeMountsField mounts={mounts} onChange={setMounts} vols={vols} loadError={volsError}/>
            <span className="hint">{t('mounts.sandboxHint')}</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="s-ttl">{t('sandboxCreate.ttl')}</label>
              <input id="s-ttl" className="input" type="number" step="any" value={Number.isNaN(ttlHours) ? '' : ttlHours}
                     aria-invalid={!ttlOK} onChange={e => setTtlHours(e.target.value === '' ? NaN : +e.target.value)}/>
              <span className="hint">{t('sandboxCreate.ttlHint')}</span>
              {!ttlOK && <span className="hint hint-error">{ttlHours > MAX_TTL_HOURS
                ? t('sandboxCreate.ttlMax', {hours: MAX_TTL_HOURS})
                : t('sandboxCreate.nonNegative')}</span>}
            </div>
            <div className="field">
              <label htmlFor="s-idle">{t('sandboxCreate.idle')}</label>
              <input id="s-idle" className="input" type="number" step="any" value={idleMinutes ?? ''} aria-invalid={!idleOK}
                     onChange={e => setIdleMinutes(e.target.value === '' ? null : +e.target.value)}
                     placeholder={opts?.idleTimeout
                       ? t('sandboxCreate.idleDefault', {value: formatGoDuration(opts.idleTimeout)})
                       : t('sandboxCreate.off')}/>
              <span className="hint">{t('sandboxCreate.idleHint', {timeout: opts?.idleTimeout ? formatGoDuration(opts.idleTimeout) : t('sandboxCreate.off')})}</span>
              {!idleOK && <span className="hint hint-error">{idleMinutes !== null && idleMinutes > MAX_TTL_HOURS * 60
                ? t('sandboxCreate.idleMax', {minutes: MAX_TTL_HOURS * 60})
                : t('sandboxCreate.nonNegative')}</span>}
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
            <div className="field">
              <label htmlFor="s-gateway">{t('sandboxCreate.gatewayPort')}</label>
              <input id="s-gateway" className="input" type="number" min={1} max={65535} step={1} value={gatewayText}
                     disabled={!gatewayOn} aria-invalid={!gatewayOK} placeholder={t('sandboxCreate.off')}
                     onChange={e => setGatewayText(e.target.value)}/>
              <span className="hint">{gatewayOn ? t('sandboxCreate.gatewayHint') : t('sandboxCreate.gatewayUnavailable')}</span>
              {!gatewayOK && <span className="hint hint-error">{t('sandboxCreate.gatewayPortRange')}</span>}
              <label htmlFor="s-ssh">{t('sandboxCreate.sshLabel')}</label>
              <select id="s-ssh" className="select" value={f.ssh ? 'yes' : 'no'}
                      onChange={e => set('ssh', e.target.value === 'yes')}>
                <option value="no">{t('sandboxCreate.sshNo')}</option>
                <option value="yes">{t('sandboxCreate.sshYes')}</option>
              </select>
              {f.ssh && <span className="hint">{t('sandboxCreate.sshHint', {max: SSH_NAME_MAX})}</span>}
            </div>
          </div>
        </form>
    </Dialog>
  )
}

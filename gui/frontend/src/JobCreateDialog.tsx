import {FormEvent, useEffect, useRef, useState} from 'react'
import {CreateJob, ListInstanceTypes, ListTemplates, TemplateNames, ListVolumes} from '../wailsjs/go/app/App'
import {app, training, volumes} from '../wailsjs/go/models'
import {envErrors, envMap, envToLines, linesToEnv, errText, generateName, MAX_RETRIES, NAME_MAX, NAME_PATTERN} from './util'
import {t} from './i18n'
import {CancelButton, Dialog} from './ui'
import {ImageField} from './ImageField'
import {Mount, VolumeMountsField, mountProblems} from './VolumeMounts'

// JobCreateDialog is the "New job" form: template prefill, instance type,
// workers, volumes, env and retry defaults.
export function JobCreateDialog({onClose, onCreated}: {
  onClose: () => void, onCreated: (name: string) => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [templates, setTemplates] = useState<string[]>([])
  // A failed read says so; an empty picker would read as a cluster with no templates.
  const [templatesError, setTemplatesError] = useState('')
  // vols is null until the project's volumes load.
  const [vols, setVols] = useState<volumes.Volume[] | null>(null)
  const [volsError, setVolsError] = useState('')
  const [f, setF] = useState(training.Form.createFrom({
    name: generateName('job'), image: '', command: '', workers: 1,
    instanceType: '', env: [], volumes: [],
    maxRetries: 0, retryOn: 'Infra',
  }))
  const [envText, setEnvText] = useState('')
  const [mounts, setMounts] = useState<Mount[]>([])

  useEffect(() => {
    ListInstanceTypes().then(ts => {
      setTypes(ts)
      // The first type is the default only while none is set, so a template's type is kept.
      if (ts.length) setF(prev => prev.instanceType ? prev : training.Form.createFrom({...prev, instanceType: ts[0].name}))
    }).catch(e => setError(errText(e)))
    TemplateNames().then(ns => { setTemplates(ns); setTemplatesError('') }, e => setTemplatesError(errText(e)))
    ListVolumes(false).then(setVols).catch(e => setVolsError(errText(e)))
  }, [])

  const set = <K extends keyof training.Form>(k: K, v: training.Form[K]) =>
    setF(prev => training.Form.createFrom({...prev, [k]: v}))

  // pickSeq numbers the template choices: a reply for an earlier choice is
  // dropped, so a slow load cannot overwrite the template chosen after it.
  const pickSeq = useRef(0)
  const useTemplate = (name: string) => {
    const seq = ++pickSeq.current
    set('template', name)
    if (!name) return
    ListTemplates().then(ts => {
      if (seq !== pickSeq.current) return
      const form = ts.find(x => x.name === name)?.form
      if (!form) return
      // The template's instance type is kept only when it names one; an empty
      // one would leave the required select blank.
      setF(prev => training.Form.createFrom({
        ...form, name: prev.name, template: name,
        instanceType: form.instanceType || prev.instanceType,
      }))
      setEnvText(envToLines(envMap(form.env || [])))
      setMounts(form.volumes || [])
    }).catch(e => {
      if (seq === pickSeq.current) setError(errText(e))
    })
  }

  // Mounts need the project's volumes to check against; without any mounts the
  // form does not wait for them.
  const mountsOK = mounts.length === 0 ||
    (vols !== null && mountProblems(mounts, vols, f.workers).every(p => !p))
  const envBad = envErrors(envText)
  // The name is a DNS label of at most 50 characters, the TrainJob name limit.
  const nameOK = NAME_PATTERN.test(f.name ?? '') && (f.name ?? '').length <= NAME_MAX
  // An empty workers field is NaN, not 0: it stays visibly empty and says so, instead of snapping to 0.
  const workersOK = Number.isInteger(f.workers) && f.workers >= 1
  // A template can name a type the project no longer offers; the backend rejects it, so Create waits.
  const typeOK = !!f.instanceType && types.some(it => it.name === f.instanceType)
  // The platform accepts 0 to 20 retries; the backend rejects anything else.
  // An empty retries field is NaN too: a cleared field is not a request for zero retries.
  const retries = f.maxRetries ?? 0
  const retriesOK = Number.isInteger(retries) && retries >= 0 && retries <= MAX_RETRIES
  // blocker says why Create is off. A bad name, worker count or env line already
  // has a hint under its field, so the footer only names the blockers that have none.
  const blocker = !f.name ? t('common.nameRequired')
    : !nameOK ? t('common.nameInvalid', {max: NAME_MAX})
    : !f.image ? t('common.imageRequired')
    : !workersOK ? t('jobCreate.workersInvalid')
    : !typeOK ? t('jobCreate.typeRequired')
    : !retriesOK ? t('jobCreate.retriesInvalid')
    : !mountsOK ? (vols === null ? (volsError ? t('mounts.loadFailed') : t('mounts.loading')) : t('mounts.fixFirst'))
    : envBad.length ? t('common.envInvalid', {lines: envBad.join(', ')}) : ''
  const hasFieldHint = (!nameOK && (f.name ?? '') !== '') || !workersOK || !retriesOK || envBad.length > 0

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (blocker) return
    setBusy(true)
    const form = training.Form.createFrom({
      ...f,
      env: Object.entries(linesToEnv(envText)).map(([name, value]) => ({name, value})),
      volumes: mounts.map(m => ({
        volume: m.volume.trim(), mountPath: m.mountPath.trim(), subPath: (m.subPath || '').trim(),
        readOnly: !!m.readOnly,
      })),
    })
    CreateJob(form).then(j => onCreated(j.name)).catch(e => {
      setError(errText(e))
      setBusy(false)
    })
  }

  return (
    <Dialog title={t('jobCreate.title')} subtitle={t('jobCreate.subtitle')}
            onClose={onClose} busy={busy} footer={<>
              {blocker && !busy && !hasFieldHint && <span className="hint hint-error" role="status">{blocker}</span>}
              <CancelButton disabled={busy}/>
              <button type="submit" form="job-form" className="btn" disabled={busy || !!blocker}>
                {busy ? t('jobCreate.creating') : t('jobCreate.create')}
              </button>
            </>}>
        <form id="job-form" onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error" role="alert">{error}</div>}
          <div className="field">
            <label htmlFor="j-name">{t('common.name')}</label>
            <input id="j-name" className="input mono" value={f.name}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} maxLength={NAME_MAX}
                   aria-invalid={!nameOK} placeholder={t('jobCreate.namePlaceholder')} required/>
            <span className="hint">{t('jobCreate.nameHint')}</span>
            {!nameOK && (f.name ?? '') !== '' && <span className="hint hint-error">{t('common.nameInvalid', {max: NAME_MAX})}</span>}
          </div>
          {templatesError && <div className="banner banner-warn" role="status">{t('jobCreate.templatesUnavailable', {error: templatesError})}</div>}
          <div className="row">
            <div className="field">
              <label htmlFor="j-template">{t('jobCreate.template')}</label>
              <select id="j-template" className="select" value={f.template || ''}
                      onChange={e => useTemplate(e.target.value)}>
                <option value="">{t('jobCreate.noTemplate')}</option>
                {templates.map(tpl => <option key={tpl} value={tpl}>{tpl}</option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="j-instance">{t('jobCreate.instanceType')}</label>
              <select id="j-instance" className="select mono" value={f.instanceType}
                      onChange={e => set('instanceType', e.target.value)} required>
                {f.instanceType && !types.some(it => it.name === f.instanceType) &&
                  <option value={f.instanceType}>{t('templates.typeUnavailable', {name: f.instanceType})}</option>}
                {types.map(it => <option key={it.name} value={it.name}>
                  {it.gpuCount
                    ? t('jobCreate.instanceOption', {
                      name: it.name, gpuCount: it.gpuCount, gpuClass: it.class,
                      cpu: it.cpu, memory: it.memory, free: it.free,
                    })
                    : t('jobCreate.instanceOptionCPU', {name: it.name, cpu: it.cpu, memory: it.memory, free: it.free})}
                </option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="j-workers">{t('jobCreate.workers')}</label>
              <input id="j-workers" className="input" type="number" min={1} value={Number.isNaN(f.workers) ? '' : f.workers}
                     onChange={e => set('workers', e.target.value === '' ? NaN : +e.target.value)} aria-invalid={!workersOK}/>
              {!workersOK && <span className="hint hint-error">{t('jobCreate.workersInvalid')}</span>}
            </div>
          </div>
          <div className="field">
            <label htmlFor="j-image">{t('common.image')}</label>
            <ImageField id="j-image" value={f.image} onChange={image => set('image', image)}
                        placeholder="cr.example.com:7443/forge-myproj/train:latest" required/>
          </div>
          <div className="field">
            <label htmlFor="j-command">{t('jobCreate.command')}</label>
            <textarea id="j-command" className="textarea" rows={4} value={f.command}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => set('command', e.target.value)}
                      placeholder="python train.py --epochs 100"/>
          </div>
          <div className="field">
            <label htmlFor="j-env">{t('jobCreate.environment')}</label>
            <textarea id="j-env" className="textarea" rows={3} value={envText}
                      autoCapitalize="none" autoCorrect="off" spellCheck={false}
                      onChange={e => setEnvText(e.target.value)} placeholder="KEY=value"
                      aria-invalid={envBad.length > 0}/>
            {envBad.length > 0 && <span className="hint hint-error">
              {t('common.envInvalid', {lines: envBad.join(', ')})}
            </span>}
          </div>
          <div className="field" role="group" aria-labelledby="j-mounts-label">
            <label id="j-mounts-label">{t('jobCreate.volumeMounts')}</label>
            <VolumeMountsField mounts={mounts} onChange={setMounts} vols={vols} loadError={volsError} workers={f.workers}/>
            <span className="hint">{t('mounts.jobHint')}</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="j-retries">{t('jobCreate.maxRetries')}</label>
              <input id="j-retries" className="input" type="number" min={0} max={MAX_RETRIES}
                     value={Number.isNaN(f.maxRetries) ? '' : f.maxRetries ?? 0} aria-invalid={!retriesOK}
                     onChange={e => set('maxRetries', e.target.value === '' ? NaN : +e.target.value)}/>
              {!retriesOK && <span className="hint hint-error">{t('jobCreate.retriesInvalid')}</span>}
            </div>
            <div className="field">
              <label htmlFor="j-retryon">{t('jobCreate.retryOn')}</label>
              <select id="j-retryon" className="select" value={f.retryOn || 'Infra'}
                      onChange={e => set('retryOn', e.target.value)}>
                <option value="Infra">{t('jobCreate.retryInfraOnly')}</option>
                <option value="Any">{t('jobCreate.retryAnyFailure')}</option>
              </select>
            </div>
          </div>
        </form>
    </Dialog>
  )
}

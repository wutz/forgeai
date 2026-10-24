import {FormEvent, useEffect, useState} from 'react'
import {CreateInferenceService, ListInstanceTypes} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, generateName, NAME_MAX, NAME_PATTERN} from './util'
import {CancelButton, Dialog} from './ui'
import {t} from './i18n'

// MODEL_PATTERN is the Hugging Face repository form the backend accepts.
const MODEL_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*\/[A-Za-z0-9][A-Za-z0-9._-]*$/

// InferenceCreateDialog is the "New inference service" form: the model, the
// instance type each replica runs on, the replica count and the vLLM arguments.
export function InferenceCreateDialog({onClose, onCreated}: {
  onClose: () => void, onCreated: (name: string) => void
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // types is null until the instance types load; the picker only offers GPU types.
  const [types, setTypes] = useState<app.InstanceType[] | null>(null)
  const [name, setName] = useState(() => generateName('inference'))
  const [model, setModel] = useState('')
  const [served, setServed] = useState('')
  const [instance, setInstance] = useState('')
  const [replicas, setReplicas] = useState(1)
  const [argsText, setArgsText] = useState('')
  const [hfSecret, setHfSecret] = useState('')

  useEffect(() => {
    ListInstanceTypes().then(ts => {
      const gpu = ts.filter(it => it.gpuCount > 0)
      setTypes(gpu)
      setInstance(cur => cur || gpu[0]?.name || '')
    }).catch(e => setError(errText(e)))
  }, [])

  const nameOK = NAME_PATTERN.test(name) && name.length <= NAME_MAX
  const modelOK = MODEL_PATTERN.test(model.trim())
  const replicasOK = Number.isInteger(replicas) && replicas >= 1
  // The footer names the blocker that has no field hint of its own, so a disabled
  // Create is never unexplained.
  const blocker = !nameOK ? t('inference.create.blockerName')
    : !modelOK ? t('inference.create.blockerModel')
    : !instance ? t('inference.create.blockerInstance')
    : !replicasOK ? t('inference.create.blockerReplicas')
    : ''
  const canSubmit = !busy && blocker === '' && types !== null

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!canSubmit) return
    setBusy(true)
    CreateInferenceService(app.InferenceForm.createFrom({
      name,
      model: model.trim(),
      servedName: served.trim(),
      instanceType: instance,
      replicas,
      args: argsText.split('\n'),
      huggingFaceSecret: hfSecret.trim(),
    })).then(s => onCreated(s.name)).catch(e => {
      setError(errText(e))
      setBusy(false)
    })
  }

  return (
    <Dialog title={t('inference.create.title')} subtitle={t('inference.create.subtitle')}
            onClose={onClose} busy={busy} footer={<>
              {blocker && !busy && <span className="hint hint-error" role="status">{blocker}</span>}
              <CancelButton disabled={busy}/>
              <button type="submit" form="inference-form" className="btn" disabled={!canSubmit}>
                {busy ? t('inference.create.creating') : t('inference.create.submit')}
              </button>
            </>}>
        <form id="inference-form" onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error" role="alert">{error}</div>}
          <div className="field">
            <label htmlFor="i-name">{t('inference.create.name')}</label>
            <input id="i-name" className="input mono" value={name} maxLength={NAME_MAX} disabled={busy}
                   onChange={e => setName(e.target.value)} aria-invalid={!nameOK}/>
            <span className="hint">{t('inference.create.nameHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="i-model">{t('inference.create.model')}</label>
            <input id="i-model" className="input mono" value={model} placeholder="Qwen/Qwen3.8-27B-FP8" disabled={busy}
                   onChange={e => setModel(e.target.value)} aria-invalid={!!model && !modelOK}/>
            <span className="hint">{t('inference.create.modelHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="i-served">{t('inference.create.served')}</label>
            <input id="i-served" className="input mono" value={served} disabled={busy}
                   placeholder={model.includes('/') ? model.split('/')[1] : ''}
                   onChange={e => setServed(e.target.value)}/>
            <span className="hint">{t('inference.create.servedHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="i-instance">{t('inference.create.instance')}</label>
            {types !== null && types.length === 0
              ? <span className="hint hint-error" role="status">{t('inference.create.noGPU')}</span>
              : <select id="i-instance" className="select mono" value={instance} disabled={busy || types === null}
                        onChange={e => setInstance(e.target.value)}>
                {(types ?? []).map(it => (
                  <option key={it.name} value={it.name}>
                    {it.name} · {it.gpuCount} × {it.product || it.class}
                  </option>
                ))}
              </select>}
            <span className="hint">{t('inference.create.instanceHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="i-replicas">{t('inference.create.replicas')}</label>
            <input id="i-replicas" className="input" type="number" min={1} step={1} value={replicas} disabled={busy}
                   onChange={e => setReplicas(e.target.valueAsNumber)}/>
          </div>
          <div className="field">
            <label htmlFor="i-args">{t('inference.create.args')}</label>
            <textarea id="i-args" className="textarea" rows={3} value={argsText} disabled={busy}
                      placeholder="--max-model-len 65536" onChange={e => setArgsText(e.target.value)}/>
            <span className="hint">{t('inference.create.argsHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="i-hf">{t('inference.create.hfSecret')}</label>
            <input id="i-hf" className="input mono" value={hfSecret} disabled={busy}
                   onChange={e => setHfSecret(e.target.value)}/>
            <span className="hint">{t('inference.create.hfSecretHint')}</span>
          </div>
        </form>
      </Dialog>
  )
}

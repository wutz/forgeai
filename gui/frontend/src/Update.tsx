import {useEffect, useState} from 'react'
import {CheckUpdate, InstallUpdate} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Icon, useConfirm} from './ui'
import {MessageKey, t} from './i18n'

// recheckEvery is how often a running client asks GitHub for a newer release.
const recheckEvery = 6 * 60 * 60 * 1000

// installSteps labels the install steps the backend reports in update:progress.
const installSteps: Record<string, MessageKey> = {
  download: 'update.downloading',
  verify: 'update.verifying',
  install: 'update.installing',
  restart: 'update.restarting',
}

// UpdateButton offers the newest forgeai release. The check is silent: it runs
// when the app starts and every few hours after, and a failed check (offline,
// GitHub unreachable) shows nothing. When a newer release is out, the top bar
// shows a button that installs it after a confirmation; the app restarts as the
// new version. Installation failures are reported through onError.
export function UpdateButton({onError}: {onError: (message: string) => void}) {
  const [info, setInfo] = useState<app.UpdateInfo | null>(null)
  // step is the install step in progress; '' when no install runs.
  const [step, setStep] = useState('')
  const confirm = useConfirm()

  useEffect(() => {
    const check = () => {
      CheckUpdate().then(setInfo).catch(() => undefined)
    }
    check()
    const timer = window.setInterval(check, recheckEvery)
    return () => window.clearInterval(timer)
  }, [])

  // The install runs in the background; its steps arrive as events.
  useEffect(() => EventsOn('update:progress', (s: string) => setStep(s)), [])

  if (!info?.available && !step) return null

  const latest = info?.latest ?? ''
  const install = () => {
    setStep('download')
    InstallUpdate().catch(e => {
      setStep('')
      onError(t('update.failed', {error: errText(e)}))
    })
  }
  const ask = () => confirm.ask({
    title: t('update.confirmTitle', {version: latest}),
    message: t('update.confirmMessage'),
    action: t('update.install'),
    primary: true,
    run: install,
  })

  return (
    <>
      {confirm.dialog}
      <button className="btn btn-accent btn-sm update-btn" onClick={ask} disabled={!!step}
              title={t('update.tooltip', {version: latest})}>
        {step
          ? <><span className="spinner" aria-hidden/>{t(installSteps[step] ?? 'update.installing')}</>
          : <><Icon name="download" size={14}/>{t('update.button', {version: latest})}</>}
      </button>
    </>
  )
}

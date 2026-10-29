import {ReactNode, useEffect, useState, useSyncExternalStore} from 'react'
import {CheckUpdate, InstallUpdate, SetMenuLabels} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Icon, useConfirm} from './ui'
import {MessageKey, getLocale, subscribeLocale, t} from './i18n'

// recheckEvery is how often a running client asks GitHub for a newer release.
const recheckEvery = 6 * 60 * 60 * 1000

// installSteps labels the install steps the backend reports in update:progress.
const installSteps: Record<string, MessageKey> = {
  download: 'update.downloading',
  verify: 'update.verifying',
  install: 'update.installing',
  restart: 'update.restarting',
}

// progressLabel is the label of the install step in progress.
function progressLabel(step: string) {
  return t(installSteps[step] ?? 'update.installing')
}

// Update is the state of the update check and install. The sidebar button and
// the application menu share one of these, so both offer the same release.
export type Update = {
  // info is the newest release as of the last check; null until one succeeds.
  info: app.UpdateInfo | null
  // step is the install step in progress; '' when no install runs.
  step: string
  // ask opens the confirmation that installs the newest release.
  ask: () => void
  // dialog is the confirmation; render it once alongside the view.
  dialog: ReactNode
}

// useUpdate checks for a newer release when the app starts and every few hours
// after. That check is silent: a failed check (offline, GitHub unreachable) shows
// nothing. The application menu's "Check for Updates" asks for a visible answer
// instead: the confirmation when a release is out, otherwise a notice through
// onNotice, or the failure through onError. Installation failures go to onError.
export function useUpdate(onError: (message: string) => void, onNotice: (message: string) => void): Update {
  const [info, setInfo] = useState<app.UpdateInfo | null>(null)
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

  // The application menu is native, so it is given its texts in the UI language.
  const locale = useSyncExternalStore(subscribeLocale, getLocale)
  useEffect(() => {
    SetMenuLabels({
      about: t('menu.about'),
      checkUpdate: t('menu.checkUpdate'),
      help: t('menu.help'),
      hide: t('menu.hide'),
      quit: t('menu.quit'),
    }).catch(() => undefined)
  }, [locale])

  // The install runs in the background; its steps arrive as events.
  useEffect(() => EventsOn('update:progress', (s: string) => setStep(s)), [])

  const install = () => {
    setStep('download')
    InstallUpdate().catch(e => {
      setStep('')
      onError(t('update.failed', {error: errText(e)}))
    })
  }
  const askFor = (version: string) => confirm.ask({
    title: t('update.confirmTitle', {version}),
    message: t('update.confirmMessage'),
    action: t('update.install'),
    primary: true,
    run: install,
  })
  const ask = () => askFor(info?.latest ?? '')

  // "Check for Updates" in the application menu.
  useEffect(() => EventsOn('update:check', () => {
    CheckUpdate().then(next => {
      setInfo(next)
      if (next.available) askFor(next.latest)
      else onNotice(next.current === 'dev' ? t('update.checkDev') : t('version.upToDate'))
    }).catch(e => onError(t('update.checkFailed', {error: errText(e)})))
  }), [])

  return {info, step, ask, dialog: confirm.dialog}
}

// SidebarUpdate is the button floating at the foot of the sidebar while a newer
// release is out or an install runs. Clicking it asks for confirmation, then
// installs.
export function SidebarUpdate({update}: {update: Update}) {
  const {info, step, ask} = update
  if (!info?.available && !step) return null

  return (
    <button className="btn btn-accent sidebar-update" onClick={ask} disabled={!!step}
            title={t('update.tooltip', {version: info?.latest ?? ''})}>
      {step
        ? <><span className="spinner" aria-hidden/>{progressLabel(step)}</>
        : <><Icon name="download" size={14}/>{t('update.sidebar')}</>}
    </button>
  )
}

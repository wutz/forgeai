import {ReactNode, useEffect, useRef, useState, useSyncExternalStore} from 'react'
import {AppVersion, CheckUpdate, InstallUpdate, SetMenuLabels} from '../wailsjs/go/app/App'
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

// Update is the state of the update check and install. The top bar button, the
// sidebar menu and the application menu share one of these, so all offer the
// same release.
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
    SetMenuLabels(t('menu.help'), t('menu.checkUpdate')).catch(() => undefined)
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

// UpdateButton is the top bar button shown while a newer release is out or an
// install runs. Clicking it asks for confirmation, then installs.
export function UpdateButton({update}: {update: Update}) {
  const {info, step, ask} = update
  if (!info?.available && !step) return null

  const latest = info?.latest ?? ''
  return (
    <button className="btn btn-accent btn-sm update-btn" onClick={ask} disabled={!!step}
            title={t('update.tooltip', {version: latest})}>
      {step
        ? <><span className="spinner" aria-hidden/>{progressLabel(step)}</>
        : <><Icon name="download" size={14}/>{t('update.button', {version: latest})}</>}
    </button>
  )
}

// VersionMenu shows the running version in the foot row of the sidebar. Its
// menu offers the newest release when one is out and installs it on click,
// through the same confirmation as the top bar button.
export function VersionMenu({update}: {update: Update}) {
  const [version, setVersion] = useState('')
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const {info, step, ask} = update

  useEffect(() => {
    AppVersion().then(setVersion).catch(() => undefined)
  }, [])

  // A click elsewhere or Escape closes the menu, as it does for the other menus.
  useEffect(() => {
    if (!open) return
    const onPointer = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  // A development build is labelled dev rather than as a release number.
  const label = version === 'dev' ? 'dev' : version ? `v${version}` : ''
  const upToDate = !!info && !info.available && info.current !== 'dev'

  return (
    <div className="pref-menu" ref={root}>
      <button className="pref-trigger version-trigger" aria-haspopup="menu" aria-expanded={open}
              aria-label={t('version.menu')} title={t('version.menu')} onClick={() => setOpen(v => !v)}>
        <span>{label}</span>
        {info?.available && <span className="update-dot" aria-hidden/>}
        <span className="pref-caret" aria-hidden><Icon name="chevron" size={11}/></span>
      </button>
      {open && (
        <div className="pref-popover version-popover" role="menu" aria-label={t('version.menu')}>
          <div className="version-row">forgeai {label}</div>
          {step ? (
            <button className="pref-item" role="menuitem" disabled>
              <span className="spinner" aria-hidden/><span>{progressLabel(step)}</span>
            </button>
          ) : info?.available ? (
            <button className="pref-item" role="menuitem" onClick={() => { setOpen(false); ask() }}>
              <Icon name="download" size={14}/><span>{t('update.button', {version: info.latest})}</span>
            </button>
          ) : upToDate ? (
            <div className="version-row">{t('version.upToDate')}</div>
          ) : null}
        </div>
      )}
    </div>
  )
}

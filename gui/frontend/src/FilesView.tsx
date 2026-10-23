import {useEffect, useState} from 'react'
import {Files} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, phaseLabel} from './util'
import {getLocale, t} from './i18n'
import {Card, Empty, PageHeader, Spinner, useActive} from './ui'

// Files shows the project's on-demand file server (dufs). It wakes the server
// and keeps it up while the view is open. The browser is embedded through a
// loopback proxy in the backend, which adds the basic-auth credentials, so the
// user signs in automatically and the password never reaches the webview.
export function FilesView() {
  const [state, setState] = useState<app.Files | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const active = useActive()

  // Polling wakes the server and keeps it up, so it stops while the view is
  // hidden and the server can go idle.
  useEffect(() => {
    if (!active) return
    let stopped = false
    let timer = 0
    // Poll quickly until the server is Ready, then at a slower pace; the
    // backend throttles the heartbeat writes either way.
    const load = () => {
      Files().then(f => {
        if (stopped) return
        setState(f)
        setError('')
        timer = window.setTimeout(load, f.phase === 'Ready' ? 15_000 : 3_000)
      }).catch(e => {
        if (stopped) return
        setError(errText(e))
        timer = window.setTimeout(load, 5_000)
      })
    }
    load()
    return () => {
      stopped = true
      window.clearTimeout(timer)
    }
  }, [attempt, active])

  const ready = state?.phase === 'Ready'
  // Only a ready server has an address: an idle one has no pod behind it.
  const frame = ready && state?.address ? state.address : ''
  // The volumes read as a list in the language of the page: "a and b", "a、b".
  // Chinese sets Latin names apart from the 和 with a thin space.
  const cjk = getLocale() === 'zh'
  const volumes = new Intl.ListFormat(getLocale(), {style: 'long', type: 'conjunction'})
    .format((state?.volumes ?? []).map(v => cjk ? ` ${v} ` : v))

  return (
    <div className="page">
      <PageHeader eyebrow={t('storage.eyebrow')} title={t('files.title')}
                  meta={state && (
                    <span className={`status status-${ready ? 'running' : state.phase.toLowerCase()}`}>
                      <span className="dot" aria-hidden/>{phaseLabel(state.phase)}
                    </span>
                  )}
                  actions={error && (
                    <button className="btn btn-sm" onClick={() => setAttempt(n => n + 1)}>{t('common.retry')}</button>
                  )}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <p className="muted note">
        {state?.volumes?.length
          ? t(ready ? 'files.serving' : 'files.servingOnWake', {volumes})
          : ''}
        {t('files.note')}
      </p>
      {!state ? (
        // Until the first answer the view says it is opening; a failed first
        // answer says so instead, while the loop keeps polling in the background.
        <Card>
          {error
            ? <Empty icon="warning" title={t('files.unavailable')}>{t('files.unavailableHint')}</Empty>
            : <Empty icon="folder" title={t('files.opening')}/>}
        </Card>
      ) : frame ? (
        <div className="files-frame card">
          <iframe src={frame} title={t('files.title')} allow="clipboard-write"/>
        </div>
      ) : ready ? (
        <Card>
          <Empty icon="folder" title={t('files.noAccess')}>
            {t('files.noAccessHint')}
          </Empty>
        </Card>
      ) : (
        <Card>
          <Spinner label={state.phase === 'Starting' ? t('files.starting') :
            state.phase === 'Idle' ? t('files.waking') : t('files.waiting')}/>
        </Card>
      )}
    </div>
  )
}

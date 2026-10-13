import {useEffect, useRef, useState} from 'react'
import {Files, Heartbeat} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText} from './util'

// Files opens the on-demand file browser (dufs): it wakes the project's
// FileServer, embeds the served URL and keeps a heartbeat going while the
// view is open. The server scales to zero when the view closes and no
// activity follows.
export function FilesView() {
  const [state, setState] = useState<app.Files | null>(null)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  const [ready, setReady] = useState(false)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    Files().then(f => {
      setState(f)
      if (f.phase === 'Ready') setReady(true)
    }).catch(e => setError(errText(e)))
    // Heartbeat every minute while the view is open: keeps the server up
    // even if the user only reads (the operator also watches the access log).
    const hb = setInterval(() => alive.current && Heartbeat().then(() =>
      Files().then(setState).catch(() => {})).catch(() => {}), 60_000)
    // Wake-up poll until Ready.
    const poll = setInterval(() => alive.current && Files().then(f => {
      setState(f)
      if (f.phase === 'Ready') setReady(true)
    }).catch(() => {}), 3_000)
    return () => {
      alive.current = false
      clearInterval(hb)
      clearInterval(poll)
    }
  }, [tick])

  const embed = state?.url && ready && state.username && state.password
    ? state.url.replace('://', `://${encodeURIComponent(state.username)}:${encodeURIComponent(state.password)}@`)
    : state?.url

  return (
    <>
      {error && <div className="banner banner-error">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Files</h1>
        {state && <span className={`status status-${state.phase.toLowerCase()}`}>
          <span className="dot" aria-hidden/>{state.phase}
        </span>}
        <span className="muted">{state?.volumes?.join(', ') || ''}</span>
        <span className="spacer"/>
        {state?.url && !embed && (
          <button className="btn" onClick={() => setTick(t => t + 1)}>Retry</button>
        )}
        {state?.url && (
          <a className="btn" href={state.url} target="_blank" rel="noreferrer">Open in browser</a>
        )}
      </div>
      <p className="muted note">
        Shared volumes are served over HTTPS and scaled to zero when idle. Uploading here counts
        as activity and keeps the server up.
      </p>
      {embed ? (
        <div className="files-frame card">
          <iframe src={embed} title="Files" allow="clipboard-write"/>
        </div>
      ) : (
        <div className="card"><div className="empty">
          {state?.phase === 'Starting' ? 'Starting the file server…' :
            state?.phase === 'Idle' ? 'Waking the file server…' : 'Waiting for the file server…'}
        </div></div>
      )}
    </>
  )
}

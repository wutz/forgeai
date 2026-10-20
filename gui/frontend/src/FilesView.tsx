import {useEffect, useState} from 'react'
import {Files} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText} from './util'
import {Card, Empty, Icon, PageHeader, Spinner} from './ui'

// Files opens the on-demand file browser (dufs): it wakes the project's
// FileServer, embeds the served URL with the basic-auth credentials and keeps
// it awake while the view is open. Files also heartbeats the server, so the
// server scales to zero once the view closes and nothing else uses it.
export function FilesView() {
  const [state, setState] = useState<app.Files | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
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
  }, [attempt])

  const ready = state?.phase === 'Ready'
  const creds = state?.username && state.password ? {user: state.username, pass: state.password} : null
  // Only a ready server with credentials is embedded. Without credentials
  // (view-only members) the iframe would prompt for a password nobody has.
  const embed = state?.url && ready && creds
    ? state.url.replace('://', `://${encodeURIComponent(creds.user)}:${encodeURIComponent(creds.pass)}@`)
    : undefined

  return (
    <div className="page">
      <PageHeader eyebrow="Storage" title="Files"
                  meta={state && (
                    <span className={`status status-${ready ? 'running' : state.phase.toLowerCase()}`}>
                      <span className="dot" aria-hidden/>{state.phase}
                    </span>
                  )}
                  actions={<>
                    {error && (
                      <button className="btn btn-sm" onClick={() => setAttempt(n => n + 1)}>Retry</button>
                    )}
                    {state?.url && (
                      <a className="btn btn-sm" href={state.url} target="_blank" rel="noreferrer">
                        <Icon name="external" size={13}/>Open in browser
                      </a>
                    )}
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <p className="muted note">
        {state?.volumes?.length ? `Serving ${state.volumes.join(', ')}. ` : ''}
        Shared volumes are served over HTTPS and scaled to zero when idle. Uploading here counts
        as activity and keeps the server up.
      </p>
      {embed ? (
        <div className="files-frame card">
          <iframe src={embed} title="Files" allow="clipboard-write"/>
        </div>
      ) : (
        <Card>
          {!state ? (
            <Empty icon="folder" title="Opening the file browser…"/>
          ) : ready && !creds ? (
            <Empty icon="folder" title="No file server password for your role">
              The server is up. Ask a project admin for access, or use Open in browser to sign in.
            </Empty>
          ) : (
            <Spinner label={state.phase === 'Starting' ? 'Starting the file server…' :
              state.phase === 'Idle' ? 'Waking the file server…' : 'Waiting for the file server…'}/>
          )}
        </Card>
      )}
    </div>
  )
}

import {useEffect, useState} from 'react'
import {Files} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText} from './util'
import {Card, Empty, PageHeader, Spinner} from './ui'

// Files shows the project's on-demand file server (dufs). It wakes the server
// and keeps it up while the view is open. The browser is embedded through a
// loopback proxy in the backend, which adds the basic-auth credentials, so the
// user signs in automatically and the password never reaches the webview.
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
  // Only a ready server has an address: an idle one has no pod behind it.
  const frame = ready && state?.address ? state.address : ''

  return (
    <div className="page">
      <PageHeader eyebrow="Storage" title="Files"
                  meta={state && (
                    <span className={`status status-${ready ? 'running' : state.phase.toLowerCase()}`}>
                      <span className="dot" aria-hidden/>{state.phase}
                    </span>
                  )}
                  actions={error && (
                    <button className="btn btn-sm" onClick={() => setAttempt(n => n + 1)}>Retry</button>
                  )}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <p className="muted note">
        {state?.volumes?.length ? `Serving ${state.volumes.join(', ')}. ` : ''}
        Shared volumes are served over HTTPS and scaled to zero when idle. Activity in the
        browser keeps the server up.
      </p>
      {!state ? (
        <Card>
          <Empty icon="folder" title="Opening the file browser…"/>
        </Card>
      ) : frame ? (
        <div className="files-frame card">
          <iframe src={frame} title="Files" allow="clipboard-write"/>
        </div>
      ) : ready ? (
        <Card>
          <Empty icon="folder" title="No file server access for your role">
            The server is up. Ask a project admin for access.
          </Empty>
        </Card>
      ) : (
        <Card>
          <Spinner label={state.phase === 'Starting' ? 'Starting the file server…' :
            state.phase === 'Idle' ? 'Waking the file server…' : 'Waiting for the file server…'}/>
        </Card>
      )}
    </div>
  )
}

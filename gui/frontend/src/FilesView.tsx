import {useEffect, useState} from 'react'
import {Files} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText} from './util'
import {Card, Empty, Icon, PageHeader, Spinner} from './ui'

// Files shows the project's on-demand file server (dufs). It wakes the server
// and keeps it up while the view is open. The files open in the browser: an
// embedded frame cannot sign in, because Chrome blocks subresources whose URL
// carries credentials, so the username and password are shown for the browser's
// sign-in prompt instead.
export function FilesView() {
  const [state, setState] = useState<app.Files | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [showPassword, setShowPassword] = useState(false)
  const [copied, setCopied] = useState('')

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

  const copy = (text: string, key: string) =>
    navigator.clipboard.writeText(text).then(() => {
      setCopied(key)
      setTimeout(() => setCopied(''), 1500)
    }, () => {})

  const ready = state?.phase === 'Ready'
  // Only a ready server offers its URL: an idle one has no pod behind it.
  const open = ready && state?.url ? state.url : ''
  const hasCreds = !!(state?.username && state.password)

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
                    {open && (
                      <a className="btn btn-sm btn-primary" href={open} target="_blank" rel="noreferrer">
                        <Icon name="external" size={13}/>Open in browser
                      </a>
                    )}
                  </>}/>
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
      ) : ready && hasCreds ? (
        <Card title="Sign in" subtitle="Enter these when the browser asks for a password">
          <div className="robot-password">
            <span className="muted">Username</span>
            <code className="mono">{state.username}</code>
            <button className="btn btn-sm" onClick={() => copy(state.username ?? '', 'user')}>
              <Icon name={copied === 'user' ? 'check' : 'copy'} size={13}/>
              {copied === 'user' ? 'Copied' : 'Copy username'}
            </button>
          </div>
          <div className="robot-password">
            <span className="muted">Password</span>
            <code className="mono">{showPassword ? state.password : '••••••••••••'}</code>
            <button className="btn btn-sm" onClick={() => setShowPassword(v => !v)}>
              {showPassword ? 'Hide' : 'Show'}
            </button>
            <button className="btn btn-sm" onClick={() => copy(state.password ?? '', 'password')}>
              <Icon name={copied === 'password' ? 'check' : 'copy'} size={13}/>
              {copied === 'password' ? 'Copied' : 'Copy password'}
            </button>
          </div>
        </Card>
      ) : ready ? (
        <Card>
          <Empty icon="folder" title="No file server password for your role">
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

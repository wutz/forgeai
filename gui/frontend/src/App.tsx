import {useCallback, useEffect, useState} from 'react'
import {Connect, Contexts, ListJobs, Resources} from '../wailsjs/go/main/App'
import {main} from '../wailsjs/go/models'
import {ConnectForm} from './ConnectForm'
import {CreateDialog} from './CreateDialog'
import {JobDetail} from './JobDetail'
import {JobTable} from './JobTable'
import {errText} from './util'

const PREFS = 'forgeai.connection'

export default function App() {
  const [contexts, setContexts] = useState<main.KubeContext[]>([])
  const [conn, setConn] = useState<main.ConnInfo | null>(null)
  const [jobs, setJobs] = useState<main.Job[]>([])
  const [resources, setResources] = useState<main.ResourceInfo[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    Contexts().then(setContexts).catch(e => setError(errText(e)))
  }, [])

  const refresh = useCallback(async () => {
    try {
      setJobs(await ListJobs())
      setError('')
    } catch (e) {
      setError(errText(e))
    }
  }, [])

  useEffect(() => {
    if (!conn) return
    refresh()
    Resources().then(setResources).catch(e => setError(errText(e)))
    const t = setInterval(refresh, 5000)
    return () => clearInterval(t)
  }, [conn, refresh])

  const connect = async (ctx: string, ns: string, user: string) => {
    setLoading(true)
    try {
      const info = await Connect(ctx, ns, user)
      localStorage.setItem(PREFS, JSON.stringify({ctx, ns, user}))
      setConn(info)
      setSelected(null)
      setError('')
    } catch (e) {
      setError(errText(e))
    } finally {
      setLoading(false)
    }
  }

  const saved = JSON.parse(localStorage.getItem(PREFS) || '{}')
  const current = jobs.find(j => j.name === selected) || null

  return (
    <div className="app">
      <header className="header">
        <div className="logo"><span className="logo-mark" aria-hidden>F</span>forgeai</div>
        {conn && (
          <div className="conn">
            <span className="conn-meta">{conn.context} · {conn.namespace}{conn.userID ? ` · ${conn.userID}` : ' · all users'}</span>
            <button className="btn btn-sm" onClick={() => setConn(null)}>Switch</button>
          </div>
        )}
      </header>

      <main className="main">
        {error && <div className="banner banner-error" role="alert">{error}</div>}
        {!conn ? (
          <ConnectForm contexts={contexts} saved={saved} loading={loading} onConnect={connect}/>
        ) : (
          <>
            <div className="toolbar">
              <h1 className="title">Training jobs</h1>
              <span className="muted">{jobs.length}</span>
              <span className="spacer"/>
              <button className="btn" onClick={refresh}>Refresh</button>
              <button className="btn btn-primary" onClick={() => setCreating(true)}>New job</button>
            </div>
            <div className={current ? 'split' : ''}>
              <JobTable jobs={jobs} selected={selected} onSelect={setSelected}/>
              {current && (
                <JobDetail job={current} onClose={() => setSelected(null)} onChanged={refresh}
                           onDeleted={() => { setSelected(null); refresh() }}/>
              )}
            </div>
          </>
        )}
      </main>

      {creating && conn && (
        <CreateDialog resources={resources} userID={conn.userID} onClose={() => setCreating(false)}
                      onCreated={j => { setCreating(false); setSelected(j.name); refresh() }}/>
      )}
    </div>
  )
}

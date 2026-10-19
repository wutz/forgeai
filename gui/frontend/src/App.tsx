import {useCallback, useEffect, useState} from 'react'
import {Connect, Contexts, Logout, Projects} from '../wailsjs/go/app/App'
import {Environment} from '../wailsjs/runtime/runtime'
import {app} from '../wailsjs/go/models'
import {Jobs} from './Jobs'
import {JobCreateDialog} from './JobCreateDialog'
import {Templates} from './Templates'
import {Volumes} from './Volumes'
import {FilesView} from './FilesView'
import {Images} from './Images'
import {Resources} from './Resources'
import {Admin} from './Admin'
import {errText} from './util'
import {Logo} from './Logo'

const PREFS = 'forgeai.connection'

type Area = 'jobs' | 'templates' | 'volumes' | 'files' | 'images' | 'resources' | 'admin'

export default function App() {
  const [contexts, setContexts] = useState<app.KubeContext[]>([])
  const [conn, setConn] = useState<app.ConnInfo | null>(null)
  const [area, setArea] = useState<Area>('jobs')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    Contexts().then(setContexts).catch(e => setError(errText(e)))
    // The macOS title bar is hidden; leave room for the window controls.
    Environment().then(env => document.body.classList.toggle('platform-darwin', env.platform === 'darwin'))
  }, [])

  const connect = async (contextName: string, namespace: string) => {
    setLoading(true)
    try {
      const info = await Connect(contextName, namespace)
      localStorage.setItem(PREFS, JSON.stringify({ctx: contextName, ns: namespace}))
      setConn(info)
      setArea(info.project ? 'jobs' : 'admin')
      setError('')
    } catch (e) {
      setError(errText(e))
    } finally {
      setLoading(false)
    }
  }

  const logout = async () => {
    await Logout()
    setConn(null)
  }

  if (!conn) return <ConnectScreen contexts={contexts} onConnect={connect} loading={loading} error={error}/>

  // Without a project (admin of a cluster with no projects yet) only the
  // admin area is usable.
  const nav: [Area, string][] = conn.project ? [
    ['jobs', 'Jobs'], ['templates', 'Templates'], ['volumes', 'Volumes'],
    ['files', 'Files'], ['images', 'Images'], ['resources', 'Resources'],
  ] : []
  if (conn.admin) nav.push(['admin', 'Projects'])

  return (
    <div className="app">
      <Header>
        <div className="conn">
          <span className="conn-meta">{conn.project ? `${conn.project} · ${conn.namespace}` : conn.context}</span>
          <button className="btn btn-sm" onClick={logout}>Switch</button>
        </div>
      </Header>
      <div className="shell">
        <nav className="sidebar">
          {nav.map(([key, label]) => (
            <button key={key} className={`nav-item ${area === key ? 'active' : ''}`}
                    onClick={() => setArea(key)}>{label}</button>
          ))}
        </nav>
        <main className="main">
          {area === 'jobs' && <Jobs/>}
          {area === 'templates' && <Templates/>}
          {area === 'volumes' && <Volumes/>}
          {area === 'files' && <FilesView/>}
          {area === 'images' && <Images/>}
          {area === 'resources' && <Resources/>}
          {area === 'admin' && <Admin/>}
        </main>
      </div>
    </div>
  )
}

function ConnectScreen({contexts, onConnect, loading, error}: {
  contexts: app.KubeContext[],
  onConnect: (ctx: string, ns: string) => void,
  loading: boolean,
  error: string,
}) {
  const saved = JSON.parse(localStorage.getItem(PREFS) || '{}')
  const [ctx, setCtx] = useState(saved.ctx || '')
  const [ns, setNs] = useState(saved.ns || '')
  // Project namespaces the context can list; empty for plain members, who
  // type their namespace (or rely on the context default).
  const [projects, setProjects] = useState<app.ProjectNamespace[]>([])

  useEffect(() => {
    if (!ctx && contexts.length) setCtx((contexts.find(c => c.current) || contexts[0]).name)
  }, [contexts])

  useEffect(() => {
    if (!ctx) return
    let live = true
    Projects(ctx).then(list => {
      if (!live) return
      setProjects(list)
      if (list.length) setNs((cur: string) => list.some(p => p.namespace === cur) ? cur : list[0].namespace)
    }).catch(() => live && setProjects([]))
    return () => { live = false }
  }, [ctx])

  return (
    <div className="app app-connect">
      <Header/>
      <main className="main">
        <div className="connect card">
          <form className="card-body" onSubmit={e => {
            e.preventDefault()
            onConnect(ctx, ns.trim())
          }}>
            <div>
              <Logo size={40}/>
              <h2>Connect to a cluster</h2>
              <p>Pick the kubeconfig context of your project. The namespace is the project
                namespace (e.g. forge-myteam); ask your admin for a kubeconfig if you don't
                have one.</p>
            </div>
            {error && <div className="banner banner-error" role="alert">{error}</div>}
            <div className="field">
              <label htmlFor="ctx">Kubeconfig context</label>
              <select id="ctx" className="select" value={ctx} onChange={e => setCtx(e.target.value)} required>
                {contexts.map(c =>
                  <option key={c.name} value={c.name}>{c.name}{c.current ? ' (current)' : ''}</option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="ns">Project namespace</label>
              {projects.length ? (
                <select id="ns" className="select mono" value={ns} onChange={e => setNs(e.target.value)}>
                  {projects.map(p =>
                    <option key={p.namespace} value={p.namespace}>{p.namespace} ({p.project})</option>)}
                </select>
              ) : (
                <input id="ns" className="input mono" value={ns} onChange={e => setNs(e.target.value)}
                       placeholder="Context default, e.g. forge-myteam"/>
              )}
              <span className="hint">Must be a forgeai project namespace.</span>
            </div>
            <button className="btn btn-primary" type="submit" disabled={loading || !ctx}>
              {loading ? 'Connecting…' : 'Connect'}
            </button>
          </form>
        </div>
      </main>
    </div>
  )
}

// Header doubles as the window title bar: it is the drag region of the
// frameless macOS window.
function Header({children}: {children?: React.ReactNode}) {
  return (
    <header className="header">
      <div className="logo"><Logo/>forgeai</div>
      {children}
    </header>
  )
}

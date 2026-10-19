import {useEffect, useState} from 'react'
import {Connect, Contexts, Logout, Projects} from '../wailsjs/go/app/App'
import {Environment} from '../wailsjs/runtime/runtime'
import {app} from '../wailsjs/go/models'
import {Overview} from './Overview'
import {Jobs} from './Jobs'
import {Sandboxes} from './Sandboxes'
import {Templates} from './Templates'
import {Volumes} from './Volumes'
import {FilesView} from './FilesView'
import {Images} from './Images'
import {Resources} from './Resources'
import {Admin} from './Admin'
import {errText} from './util'
import {Logo} from './Logo'
import {HeaderSlot, Icon, IconName} from './ui'

const PREFS = 'forgeai.connection'
const SIDEBAR = 'forgeai.sidebar'

type Area = 'overview' | 'jobs' | 'sandboxes' | 'templates' | 'volumes' | 'files' | 'images' | 'resources' | 'admin'

type NavItem = {area: Area, label: string, icon: IconName}

export default function App() {
  const [contexts, setContexts] = useState<app.KubeContext[]>([])
  const [conn, setConn] = useState<app.ConnInfo | null>(null)
  const [area, setArea] = useState<Area>('overview')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  // The rail remembers its state across launches, like the connection does.
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(SIDEBAR) === 'collapsed')
  // Views portal their PageHeader into this node in the top bar.
  const [headerSlot, setHeaderSlot] = useState<HTMLDivElement | null>(null)

  const toggleSidebar = () => setCollapsed(v => {
    localStorage.setItem(SIDEBAR, v ? 'open' : 'collapsed')
    return !v
  })

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
      setArea(info.project ? 'overview' : 'admin')
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
  // The sidebar groups areas like the reference consoles: workspace, storage,
  // cluster catalogue, then admin. Sandboxes appear only when the connected
  // cluster runs agents.x-k8s.io.
  const groups: {label: string, items: NavItem[]}[] = conn.project ? [
    {
      label: 'Workspace',
      items: [
        {area: 'overview', label: 'Overview', icon: 'grid'},
        {area: 'jobs', label: 'Jobs', icon: 'play'},
        ...(conn.sandboxes ? [{area: 'sandboxes', label: 'Sandboxes', icon: 'box'} as NavItem] : []),
        {area: 'templates', label: 'Templates', icon: 'scroll'},
      ],
    },
    {
      label: 'Storage',
      items: [
        {area: 'volumes', label: 'Volumes', icon: 'database'},
        {area: 'files', label: 'Files', icon: 'folder'},
        {area: 'images', label: 'Images', icon: 'image'},
      ],
    },
    {
      label: 'Cluster',
      items: [{area: 'resources', label: 'Resources', icon: 'cpu'}],
    },
  ] : []
  if (conn.admin) {
    groups.push({label: 'Admin', items: [{area: 'admin', label: 'Projects', icon: 'shield'}]})
  }

  return (
    <HeaderSlot.Provider value={headerSlot}>
      <div className={`app ${collapsed ? 'sidebar-collapsed' : ''}`}>
        <header className="header header-shell">
          <div className="header-side">
            <button className="icon-btn header-toggle" onClick={toggleSidebar}
                    aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
                    aria-expanded={!collapsed} title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
              <Icon name="panel"/>
            </button>
          </div>
          <div className="header-main">
            <div className="header-slot" ref={setHeaderSlot}/>
            <div className="conn">
              <span className="conn-meta">{conn.project ? `${conn.project} · ${conn.namespace}` : conn.context}</span>
              <button className="btn btn-sm" onClick={logout}>Switch</button>
            </div>
          </div>
        </header>
        <div className={`shell ${collapsed ? 'collapsed' : ''}`}>
          <nav className="sidebar">
            {groups.map(g => (
              <div className="nav-group" key={g.label}>
                <div className="nav-label">{g.label}</div>
                {g.items.map(item => (
                  <button key={item.area} className={`nav-item ${area === item.area ? 'active' : ''}`}
                          onClick={() => setArea(item.area)}
                          title={collapsed ? item.label : undefined}
                          aria-label={item.label} aria-current={area === item.area ? 'page' : undefined}>
                    <Icon name={item.icon}/><span>{item.label}</span>
                  </button>
                ))}
              </div>
            ))}
            <div className="nav-foot">
              <span className="conn-meta" title={conn.server}>{conn.context}</span>
              <span className="conn-meta" title={conn.namespace}>{conn.namespace || 'no namespace'}</span>
            </div>
          </nav>
          <main className="main">
            {area === 'overview' && <Overview onOpen={setArea}/>}
            {area === 'jobs' && <Jobs/>}
            {area === 'sandboxes' && <Sandboxes/>}
            {area === 'templates' && <Templates/>}
            {area === 'volumes' && <Volumes/>}
            {area === 'files' && <FilesView/>}
            {area === 'images' && <Images/>}
            {area === 'resources' && <Resources/>}
            {area === 'admin' && <Admin/>}
          </main>
        </div>
      </div>
    </HeaderSlot.Provider>
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

  // Fall back to the current context when the saved one is gone from the
  // kubeconfig (a removed or renamed context would otherwise be submitted
  // while the select shows the first option).
  useEffect(() => {
    if (!contexts.length) return
    if (!ctx || !contexts.some(c => c.name === ctx)) {
      setCtx((contexts.find(c => c.current) || contexts[0]).name)
    }
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
// frameless macOS window. The connect screen has no sidebar, so the band
// stays empty and the card carries the wordmark.
function Header() {
  return <header className="header"/>
}

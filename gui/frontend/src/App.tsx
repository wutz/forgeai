import {Fragment, useEffect, useRef, useState} from 'react'
import {Connect, Contexts, Projects} from '../wailsjs/go/app/App'
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
import {Dependencies} from './Dependencies'
import {Scope} from './Scope'
import {errText} from './util'
import {Empty, HeaderSlot, Icon, IconName, Spinner} from './ui'

// PREFS is the last connection. NAMESPACES maps each kubeconfig context to the
// project last opened in it, so switching clusters returns to that project.
const PREFS = 'forgeai.connection'
const NAMESPACES = 'forgeai.namespaces'
const SIDEBAR = 'forgeai.sidebar'

type Area = 'overview' | 'jobs' | 'sandboxes' | 'templates' | 'volumes' | 'files' | 'images' | 'resources'
  | 'admin' | 'dependencies'

type NavItem = {area: Area, label: string, icon: IconName}

// pickNamespace chooses the project a context opens with: the requested one,
// else the one last used there, else the first project. Users that may not
// list projects get no list, so the requested namespace is taken as typed.
// With nothing to go on it returns '' and the context default is used.
function pickNamespace(list: app.ProjectNamespace[], requested: string, remembered: string): string {
  if (!list.length) return requested || remembered
  const known = [requested, remembered].find(ns => list.some(p => p.namespace === ns))
  return known ?? list[0].namespace
}

// adminAreas lists the admin areas the connection offers: Projects needs the
// forgeai CRDs and admin rights, Dependencies needs cluster-admin.
function adminAreas(info: app.ConnInfo): Area[] {
  return [
    ...(info.admin && info.forgeai ? ['admin' as const] : []),
    ...(info.clusterAdmin ? ['dependencies' as const] : []),
  ]
}

// areaFor keeps the open view when the new connection offers it. A connection
// without a project only has the admin areas, and opens on the first of them.
function areaFor(current: Area, info: app.ConnInfo): Area {
  const offered = adminAreas(info)
  if (!info.project) return offered.includes(current) ? current : offered[0] ?? 'dependencies'
  if (offered.includes(current)) return current
  if (current === 'admin' || current === 'dependencies') return 'overview'
  if (current === 'sandboxes' && !info.sandboxes) return 'overview'
  return current
}

export default function App() {
  const [contexts, setContexts] = useState<app.KubeContext[]>([])
  // cluster is the kubeconfig context shown in the sidebar. It leads the
  // connection while a switch is in flight.
  const [cluster, setCluster] = useState('')
  const [projects, setProjects] = useState<app.ProjectNamespace[]>([])
  const [conn, setConn] = useState<app.ConnInfo | null>(null)
  const [area, setArea] = useState<Area>('overview')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  // The rail remembers its state across launches, like the connection does.
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(SIDEBAR) === 'collapsed')
  // Views portal their PageHeader into this node in the top bar.
  const [headerSlot, setHeaderSlot] = useState<HTMLDivElement | null>(null)
  // Each connect takes a number; a slower, superseded switch must not win.
  const attempt = useRef(0)
  const started = useRef(false)

  const toggleSidebar = () => setCollapsed(v => {
    localStorage.setItem(SIDEBAR, v ? 'open' : 'collapsed')
    return !v
  })

  const connectTo = async (contextName: string, requested = '') => {
    const seq = ++attempt.current
    const remembered: Record<string, string> = JSON.parse(localStorage.getItem(NAMESPACES) || '{}')
    setCluster(contextName)
    setLoading(true)
    setError('')
    try {
      const list = await Projects(contextName).catch(() => [] as app.ProjectNamespace[])
      const info = await Connect(contextName, pickNamespace(list, requested, remembered[contextName] || ''))
      if (seq !== attempt.current) return
      localStorage.setItem(PREFS, JSON.stringify({ctx: contextName, ns: info.namespace}))
      localStorage.setItem(NAMESPACES, JSON.stringify({...remembered, [contextName]: info.namespace}))
      setProjects(list)
      setConn(info)
      setArea(current => areaFor(current, info))
    } catch (e) {
      if (seq !== attempt.current) return
      setError(errText(e))
      // Keep the sidebar on the cluster that is actually connected.
      setCluster(current => (conn ? conn.context : current))
    } finally {
      if (seq === attempt.current) setLoading(false)
    }
  }

  useEffect(() => {
    // StrictMode runs effects twice in development; connect once.
    if (started.current) return
    started.current = true
    // The macOS title bar is hidden; leave room for the window controls.
    Environment().then(env => document.body.classList.toggle('platform-darwin', env.platform === 'darwin'))
    Contexts().then(list => {
      setContexts(list)
      const last: {ctx?: string, ns?: string} = JSON.parse(localStorage.getItem(PREFS) || '{}')
      const start = list.find(c => c.name === last.ctx) || list.find(c => c.current) || list[0]
      if (!start) {
        setError('No kubeconfig context found. Add one to $KUBECONFIG or ~/.kube/config.')
        return
      }
      connectTo(start.name, start.name === last.ctx ? last.ns : '')
    }).catch(e => setError(errText(e)))
  }, [])

  const switchCluster = (name: string) => {
    if (name !== conn?.context) connectTo(name)
  }

  const switchProject = (namespace: string) => {
    if (conn && namespace !== conn.namespace) connectTo(conn.context, namespace)
  }

  // The sidebar groups areas like the reference consoles: workspace, storage,
  // cluster catalogue, then admin. Sandboxes appear only when the connected
  // cluster runs agents.x-k8s.io.
  const groups: {label: string, items: NavItem[]}[] = conn?.project ? [
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
  const admin: NavItem[] = conn ? adminAreas(conn).map(a => a === 'admin'
    ? {area: a, label: 'Projects', icon: 'shield'}
    : {area: a, label: 'Dependencies', icon: 'layers'}) : []
  if (admin.length) groups.push({label: 'Admin', items: admin})

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
          </div>
        </header>
        <div className={`shell ${collapsed ? 'collapsed' : ''}`}>
          <nav className="sidebar">
            <Scope contexts={contexts} cluster={cluster} projects={projects} namespace={conn?.namespace ?? ''}
                   connected={!!conn} busy={loading} onCluster={switchCluster} onProject={switchProject}/>
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
          </nav>
          <main className="main">
            {error && <div className="banner banner-error" role="alert">{error}</div>}
            {conn ? (
              // Keyed by the connection so every view refetches after a switch.
              <Fragment key={`${conn.context}/${conn.namespace}`}>
                {area === 'overview' && <Overview onOpen={setArea}/>}
                {area === 'jobs' && <Jobs/>}
                {area === 'sandboxes' && <Sandboxes/>}
                {area === 'templates' && <Templates sandboxes={conn.sandboxes}/>}
                {area === 'volumes' && <Volumes/>}
                {area === 'files' && <FilesView/>}
                {area === 'images' && <Images/>}
                {area === 'resources' && <Resources/>}
                {area === 'admin' && <Admin/>}
                {area === 'dependencies' && <Dependencies/>}
              </Fragment>
            ) : loading ? (
              <Spinner label="Connecting…"/>
            ) : (
              <Empty icon="server" title="Not connected">
                Choose a cluster and project in the sidebar.
              </Empty>
            )}
          </main>
        </div>
      </div>
    </HeaderSlot.Provider>
  )
}

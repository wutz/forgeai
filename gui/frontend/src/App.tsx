import {ReactNode, useEffect, useRef, useState, useSyncExternalStore} from 'react'
import {Connect, Contexts, Projects} from '../wailsjs/go/app/App'
import {Environment} from '../wailsjs/runtime/runtime'
import {app} from '../wailsjs/go/models'
import {Overview} from './Overview'
import {Jobs} from './Jobs'
import {Inference} from './Inference'
import {Sandboxes} from './Sandboxes'
import {Volumes} from './Volumes'
import {FilesView} from './FilesView'
import {Images} from './Images'
import {Resources} from './Resources'
import {Admin} from './Admin'
import {Dependencies} from './Dependencies'
import {Scope} from './Scope'
import {errText} from './util'
import {CreateAccessContext, Empty, HeaderSlot, Icon, IconName, Pane, Spinner} from './ui'
import {Preferences} from './Preferences'
import {TemplateSourcesDialog} from './TemplateSourcesDialog'
import {UpdateButton, useUpdate} from './Update'
import {getLocale, subscribeLocale, t} from './i18n'

// PREFS is the last connection. NAMESPACES maps each kubeconfig context to the
// project last opened in it, so switching clusters returns to that project.
const PREFS = 'forgeai.connection'
const NAMESPACES = 'forgeai.namespaces'
const SIDEBAR = 'forgeai.sidebar'
// Matches the redesign.css breakpoint where the rail becomes an overlay.
const NARROW = '(max-width: 820px)'
const isNarrow = () => window.matchMedia(NARROW).matches

type Area = 'overview' | 'jobs' | 'sandboxes' | 'inference' | 'volumes' | 'files' | 'images' | 'resources'
  | 'admin' | 'dependencies'

type NavItem = {area: Area, label: string, icon: IconName}

// readJSON reads a JSON value from localStorage. A damaged entry counts as
// absent, so it cannot stop the connection that reads it.
function readJSON<T>(key: string, fallback: T): T {
  try {
    return JSON.parse(localStorage.getItem(key) || 'null') ?? fallback
  } catch {
    return fallback
  }
}

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

// nothingOpen reports a connection with neither a project nor an admin area:
// there is no view to show, so the page says why instead.
function nothingOpen(info: app.ConnInfo): boolean {
  return !info.project && adminAreas(info).length === 0
}

// areaFor keeps the open view when the new connection offers it. A connection
// without a project only has the admin areas, and opens on the first of them.
function areaFor(current: Area, info: app.ConnInfo): Area {
  const offered = adminAreas(info)
  if (!info.project) return offered.includes(current) ? current : offered[0] ?? 'overview'
  if (offered.includes(current)) return current
  if (current === 'admin' || current === 'dependencies') return 'overview'
  if (current === 'sandboxes' && !info.sandboxes) return 'overview'
  if (current === 'inference' && !info.inference) return 'overview'
  return current
}

export default function App() {
  // Subscribing re-renders the whole tree when the language is switched, so
  // every t() call below picks up the new catalog.
  useSyncExternalStore(subscribeLocale, getLocale)
  const [contexts, setContexts] = useState<app.KubeContext[]>([])
  // cluster is the kubeconfig context shown in the sidebar. It leads the
  // connection while a switch is in flight.
  const [cluster, setCluster] = useState('')
  const [projects, setProjects] = useState<app.ProjectNamespace[]>([])
  const [conn, setConn] = useState<app.ConnInfo | null>(null)
  const [area, setArea] = useState<Area>('overview')
  // sourcesOpen shows the template sources dialog, opened from the sidebar.
  const [sourcesOpen, setSourcesOpen] = useState(false)
  // jobFocus asks the Jobs page to select a job; a new object each time, so the same job can be picked again.
  const [jobFocus, setJobFocus] = useState<{name: string} | null>(null)
  // visited lists the areas opened on this connection. A view mounts on its
  // first visit and stays mounted after that.
  const [visited, setVisited] = useState<Area[]>([])
  const [error, setError] = useState('')
  const update = useUpdate(setError)
  const [loading, setLoading] = useState(false)
  // The rail remembers its state across launches, like the connection does.
  // A narrow window overlays the page with the rail, so it always starts closed.
  const [collapsed, setCollapsed] = useState(() => isNarrow() || localStorage.getItem(SIDEBAR) === 'collapsed')
  // Views portal their PageHeader into this node in the top bar.
  const [headerSlot, setHeaderSlot] = useState<HTMLDivElement | null>(null)
  // Each connect takes a number; a slower, superseded switch must not win.
  const attempt = useRef(0)
  const started = useRef(false)

  const toggleSidebar = () => setCollapsed(v => {
    localStorage.setItem(SIDEBAR, v ? 'open' : 'collapsed')
    return !v
  })
  // Crossing the breakpoint re-applies the state for that width: a narrow window
  // closes the overlay, and a wide one goes back to the rail the user saved, so an
  // overlay closed on a phone does not leave the desktop rail hidden.
  useEffect(() => {
    const query = window.matchMedia(NARROW)
    const sync = () => setCollapsed(query.matches || localStorage.getItem(SIDEBAR) === 'collapsed')
    query.addEventListener('change', sync)
    return () => query.removeEventListener('change', sync)
  }, [])

  const connectTo = async (contextName: string, requested = '') => {
    const seq = ++attempt.current
    const remembered = readJSON<Record<string, string>>(NAMESPACES, {})
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
      const last = readJSON<{ctx?: string, ns?: string}>(PREFS, {})
      const start = list.find(c => c.name === last.ctx) || list.find(c => c.current) || list[0]
      if (!start) {
        setError(t('app.noContext'))
        return
      }
      connectTo(start.name, start.name === last.ctx ? last.ns : '')
    }).catch(e => setError(errText(e)))
  }, [])

  // The current area counts as visited on the render it opens, so its view
  // mounts at once; the effect records it for the renders after.
  useEffect(() => {
    setVisited(v => v.includes(area) ? v : [...v, area])
  }, [area])
  const opened = visited.includes(area) ? visited : [...visited, area]
  const shown = (a: Area, view: ReactNode) => opened.includes(a) && <Pane on={area === a}>{view}</Pane>

  const switchCluster = (name: string) => {
    if (name !== conn?.context) connectTo(name)
  }

  const switchProject = (namespace: string) => {
    if (conn && namespace !== conn.namespace) connectTo(conn.context, namespace)
  }

  // refreshProjects re-reads the connected cluster's projects after an admin
  // created, saved or deleted one. If the connected project is gone, it connects
  // to a project that is left.
  const refreshProjects = () => {
    if (!conn) return
    const context = conn.context
    Projects(context).then(list => {
      setProjects(list)
      if (!list.some(p => p.namespace === conn.namespace)) connectTo(context)
    }).catch(() => undefined)
  }

  // The sidebar groups areas like the reference consoles: workspace, storage,
  // cluster catalogue, then admin. Sandboxes appear only when the connected
  // cluster runs agents.x-k8s.io. Templates are reached from the tab row of
  // their Sandboxes and Jobs pages.
  const groups: {label: string, items: NavItem[]}[] = conn?.project ? [
    {
      label: t('nav.workspace'),
      items: [
        {area: 'overview', label: t('nav.overview'), icon: 'grid'},
        ...(conn.sandboxes ? [{area: 'sandboxes', label: t('nav.sandboxes'), icon: 'box'} as NavItem] : []),
        {area: 'jobs', label: t('nav.jobs'), icon: 'play'},
        ...(conn.inference ? [{area: 'inference', label: t('nav.inference'), icon: 'bolt'} as NavItem] : []),
      ],
    },
    {
      label: t('nav.storage'),
      items: [
        {area: 'volumes', label: t('nav.volumes'), icon: 'database'},
        {area: 'files', label: t('nav.files'), icon: 'folder'},
        {area: 'images', label: t('nav.images'), icon: 'image'},
      ],
    },
    {
      label: t('nav.cluster'),
      items: [{area: 'resources', label: t('nav.resources'), icon: 'cpu'}],
    },
  ] : []
  const admin: NavItem[] = conn ? adminAreas(conn).map(a => a === 'admin'
    ? {area: a, label: t('nav.projects'), icon: 'shield'}
    : {area: a, label: t('nav.dependencies'), icon: 'layers'}) : []
  if (admin.length) groups.push({label: t('nav.admin'), items: admin})

  return (
    <HeaderSlot.Provider value={headerSlot}>
      <div className={`app ${collapsed ? 'sidebar-collapsed' : ''}`}>
        <header className="header header-shell">
          <div className="header-side">
            <button className="icon-btn header-toggle" onClick={toggleSidebar}
                    aria-label={collapsed ? t('app.expandSidebar') : t('app.collapseSidebar')}
                    aria-expanded={!collapsed} title={collapsed ? t('app.expandSidebar') : t('app.collapseSidebar')}>
              <Icon name="panel"/>
            </button>
          </div>
          <div className="header-main">
            <div className="header-slot" ref={setHeaderSlot}/>
            <UpdateButton update={update}/>
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
                          onClick={() => {
                            setArea(item.area)
                            // On a narrow window the rail is an overlay; picking a page dismisses it.
                            if (isNarrow()) setCollapsed(true)
                          }}
                          title={collapsed ? item.label : undefined}
                          aria-label={item.label} aria-current={area === item.area ? 'page' : undefined}>
                    <Icon name={item.icon}/><span>{item.label}</span>
                  </button>
                ))}
              </div>
            ))}
            <Preferences update={update} onManageSources={() => setSourcesOpen(true)}/>
          </nav>
          {sourcesOpen && <TemplateSourcesDialog onClose={() => setSourcesOpen(false)}/>}
          <main className="main">
            {error && <div className="banner banner-error" role="alert">{error}</div>}
            {conn && nothingOpen(conn) ? (
              <Empty icon="shield" title={t('app.noAccess')}>
                {t('app.noAccessHint')}
              </Empty>
            ) : conn ? (
              // Keyed by the connection so every view refetches after a switch.
              <CreateAccessContext.Provider key={`${conn.context}/${conn.namespace}`} value={conn.create}>
                {shown('overview', <Overview onOpen={setArea} onOpenJob={name => { setJobFocus({name}); setArea('jobs') }}/>)}
                {shown('jobs', <Jobs focus={jobFocus}/>)}
                {shown('sandboxes', <Sandboxes/>)}
                {shown('inference', <Inference/>)}
                {shown('volumes', <Volumes/>)}
                {shown('files', <FilesView/>)}
                {shown('images', <Images/>)}
                {shown('resources', <Resources/>)}
                {shown('admin', <Admin onProjectsChanged={refreshProjects}/>)}
                {shown('dependencies', <Dependencies/>)}
              </CreateAccessContext.Provider>
            ) : loading ? (
              <Spinner label={t('app.connecting')}/>
            ) : (
              <Empty icon="server" title={t('app.notConnected')}>
                {t('app.chooseHint')}
              </Empty>
            )}
          </main>
        </div>
        {update.dialog}
      </div>
    </HeaderSlot.Provider>
  )
}

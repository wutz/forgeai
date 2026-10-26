// Shared UI primitives: icon set, page header, metric tiles, meters,
// segmented filters, tabs, badges, empty state and dialogs. Everything here
// renders from tokens in style.css (gui/DESIGN.md).

import {KeyboardEvent as ReactKeyboardEvent, ReactNode, createContext, useContext, useEffect, useId, useLayoutEffect, useRef, useState, useSyncExternalStore} from 'react'
import {createPortal} from 'react-dom'
import {app} from '../wailsjs/go/models'
import {t} from './i18n'

// HeaderSlot is the node in the app header that PageHeader renders into, so
// every view shows its title and actions in the top bar instead of repeating
// a title band at the top of the content.
export const HeaderSlot = createContext<HTMLElement | null>(null)

// ActiveView is true while the view is on screen. Views stay mounted when the
// user switches away, so they stop their timers while it is false and refresh
// when it turns true again.
export const ActiveView = createContext(true)
export const useActive = () => useContext(ActiveView)

// CreateAccessContext is what the user may create in the project, from the
// connection. Views show a create button only when its kind is allowed.
export const CreateAccessContext = createContext<app.CreateAccess | null>(null)
const noCreate: app.CreateAccess = {
  jobs: false, jobTemplates: false, volumes: false, inference: false, sandboxes: false, sandboxTemplates: false,
}
export const useCreateAccess = (): app.CreateAccess => useContext(CreateAccessContext) ?? noCreate

// Pane keeps a view mounted while another view is on screen, so the view keeps
// its selections, filters and drafts. Only the view on screen owns the header.
export function Pane({on, children}: {on: boolean, children: ReactNode}) {
  const slot = useContext(HeaderSlot)
  return (
    <ActiveView.Provider value={on}>
      <HeaderSlot.Provider value={on ? slot : null}>
        <div hidden={!on}>{children}</div>
      </HeaderSlot.Provider>
    </ActiveView.Provider>
  )
}

// Icon is a 16px stroke icon (Lucide-style paths, 1.75 stroke).
const ICONS = {
  grid: 'M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z',
  cpu: 'M9 3v2M15 3v2M9 19v2M15 19v2M3 9h2M3 15h2M19 9h2M19 15h2M6 6h12v12H6zM9.5 9.5h5v5h-5z',
  layers: 'm12 2 9 5-9 5-9-5 9-5M3 12l9 5 9-5M3 17l9 5 9-5',
  folder: 'M3 6h6l2 2h10v11H3z',
  box: 'M21 8 12 3 3 8v8l9 5 9-5V8zM3 8l9 5 9-5M12 13v8',
  server: 'M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01',
  shield: 'M12 3l8 3v6c0 4.4-3.4 8.4-8 9-4.6-.6-8-4.6-8-9V6l8-3z',
  play: 'M7 4l13 8-13 8z',
  square: 'M5 5h14v14H5z',
  scroll: 'M6 3h12v18H6zM9 8h6M9 12h6M9 16h4',
  refresh: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7',
  plus: 'M12 5v14M5 12h14',
  trash: 'M4 7h16M9 7V4h6v3M6 7l1 14h10l1-14M10 11v6M14 11v6',
  external: 'M14 4h6v6M20 4l-9 9M18 14v5H5V6h5',
  copy: 'M9 9h11v11H9zM5 15H4V4h11v1',
  close: 'M6 6l12 12M18 6 6 18',
  chevron: 'm9 6 6 6-6 6',
back: 'M15 18l-6-6 6-6',
  download: 'M12 4v11M7 10l5 5 5-5M5 20h14',
  clock: 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z',
  bolt: 'M13 2 4 14h7l-1 8 9-12h-7z',
  database: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
  image: 'M4 5h16v14H4zM4 15l5-5 4 4 3-3 4 4M15 9h.01',
  check: 'm5 12 4.5 4.5L19 7',
  warning: 'M12 4 2.5 20h19L12 4zM12 10v4M12 17h.01',
  panel: 'M3 4h18v16H3zM9 4v16',
  terminal: 'M4 5h16v14H4zM7.5 10l2.5 2-2.5 2M12.5 15h4',
  maximize: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5',
  minimize: 'M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5',
  monitor: 'M3 4h18v12H3zM8 20h8M12 16v4',
  sun: 'M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0zM12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M5.6 18.4 7 17M17 7l1.4-1.4',
  moon: 'M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z',
} as const

export type IconName = keyof typeof ICONS

export function Icon({name, size = 16}: {name: IconName; size?: number}) {
  return (
    <svg className="icon" width={size} height={size} viewBox="0 0 24 24" fill="none"
         stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round"
         aria-hidden>
      <path d={ICONS[name]}/>
    </svg>
  )
}

// useDetailClose is the close of a list's open detail. The detail's close button
// goes with the detail, so focus returns to the row it was opened from, and the
// returned split ref is put on the element that holds the list.
export function useDetailClose(selected: string | null, select: (name: string | null) => void) {
  const split = useRef<HTMLDivElement>(null)
  const returnTo = useRef<string | null>(null)
  useEffect(() => {
    if (selected || !returnTo.current) return
    const name = returnTo.current
    returnTo.current = null
    const rows = split.current?.querySelectorAll<HTMLButtonElement>('button.row-name') ?? []
    Array.from(rows).find(b => b.textContent === name)?.focus()
  }, [selected])
  const close = () => {
    returnTo.current = selected
    select(null)
  }
  return {split, close}
}

// phoneQuery matches windows too narrow for a page's actions to share the top bar.
const phoneQuery = typeof window === 'undefined' ? null : window.matchMedia('(max-width: 560px)')
const subscribePhone = (fn: () => void) => {
  phoneQuery?.addEventListener('change', fn)
  return () => phoneQuery?.removeEventListener('change', fn)
}

// PageHeader is the title band of every view. It renders into the app header
// (HeaderSlot) so the top bar carries the title, optional inline meta and the
// actions; views outside a header fall back to a band above the content. On a
// phone-width window the band stays in the page, since the top bar has no room
// for the actions beside the title.
export function PageHeader({eyebrow, title, meta, actions}: {
  eyebrow?: string, title: string, meta?: ReactNode, actions?: ReactNode
}) {
  const slot = useContext(HeaderSlot)
  const phone = useSyncExternalStore(subscribePhone, () => !!phoneQuery?.matches, () => false)
  const content = (
    <>
      <div className="page-head-text">
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <div className="page-head-row">
          <h1 className="page-title">{title}</h1>
          {meta ? <span className="page-meta">{meta}</span> : null}
        </div>
      </div>
      {actions && <div className="page-head-actions">{actions}</div>}
    </>
  )
  return slot && !phone
    ? createPortal(<div className="page-head in-header">{content}</div>, slot)
    : <div className="page-head">{content}</div>
}

// LogView shows a read log. Long lines wrap. The box follows new output while
// the reader is at the end and keeps their place once they have scrolled up.
// text is null until the first read, and '' when the log has no output.
export function LogView({title, text, onRefresh}: {
  title?: ReactNode, text: string | null, onRefresh: () => Promise<unknown>
}) {
  const box = useRef<HTMLPreElement>(null)
  const following = useRef(true)
  const [copied, setCopied] = useState(false)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])

  useLayoutEffect(() => {
    const el = box.current
    if (el && following.current) el.scrollTop = el.scrollHeight
  }, [text])

  const onScroll = () => {
    const el = box.current
    if (el) following.current = el.scrollHeight - el.scrollTop - el.clientHeight < 16
  }

  const copy = () => {
    if (!text) return
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setCopied(false), 1500)
    }, () => {})
  }

  return (
    <>
      <div className="log-bar">
        {title && <span className="mono muted log-title">{title}</span>}
        <span className="spacer"/>
        <button className="btn btn-sm btn-ghost" onClick={copy} disabled={!text}>
          <Icon name={copied ? 'check' : 'copy'} size={13}/>{copied ? t('common.copied') : t('common.copy')}
        </button>
        <RefreshButton label="Reload" onRefresh={onRefresh}/>
      </div>
      {text === null
        ? <div className="logs logs-empty"><span>{t('common.logLoading')}</span></div>
        : text === ''
          ? <div className="logs logs-empty"><Icon name="scroll" size={18}/><span>{t('common.logEmpty')}</span></div>
          : <pre ref={box} className="logs" tabIndex={0} aria-label={t('common.logs')} onScroll={onScroll}>{text}</pre>}
    </>
  )
}

export function Card({title, subtitle, actions, children, className = ''}: {
  title?: ReactNode, subtitle?: ReactNode, actions?: ReactNode, children: ReactNode, className?: string
}) {
  return (
    <section className={`card ${className}`}>
      {(title || actions) && (
        <div className="card-head">
          <div>
            {title && <h2 className="card-title">{title}</h2>}
            {subtitle && <div className="card-sub">{subtitle}</div>}
          </div>
          {actions && <div className="card-head-actions">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  )
}

// MetricTile is the large-number tile of the overview: mono eyebrow, big
// value with a unit, a detail line and an optional meter.
export function MetricTile({icon, label, value, unit, detail, tone, children}: {
  icon?: IconName, label: string, value: ReactNode, unit?: string, detail?: ReactNode,
  tone?: 'accent' | 'success' | 'warning' | 'error' | 'violet', children?: ReactNode
}) {
  return (
    <div className={`metric ${tone ? `metric-${tone}` : ''}`}>
      <div className="metric-label">
        {icon && <span className="metric-icon"><Icon name={icon} size={14}/></span>}
        {label}
      </div>
      <div className="metric-value">{value}{unit && <span className="metric-unit">{unit}</span>}</div>
      {detail && <div className="metric-detail">{detail}</div>}
      {children}
    </div>
  )
}

// Meter is a thin progress bar with an optional label row and a tone.
export function Meter({value, max, tone = 'accent', label, right}: {
  value: number, max: number, tone?: 'accent' | 'success' | 'warning' | 'error' | 'muted',
  label?: ReactNode, right?: ReactNode
}) {
  const pct = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0
  return (
    <div className="meter">
      {(label || right) && <div className="meter-row"><span>{label}</span><span>{right}</span></div>}
      <div className="meter-track" role="progressbar" aria-valuenow={Math.round(pct)}
           aria-valuemin={0} aria-valuemax={100} aria-label={typeof label === 'string' ? label : undefined}>
        <div className={`meter-fill meter-${tone}`} style={{width: `${pct}%`}}/>
      </div>
    </div>
  )
}

// tabKeyDown gives a tab row the ARIA tabs keys: the arrows, Home and End move
// the selection and the focus together. Only the selected tab is in the Tab
// order (see tabIndex at the call sites), so the row costs one stop.
function tabKeyDown<T extends string>(e: ReactKeyboardEvent<HTMLDivElement>, values: T[], onChange: (v: T) => void) {
  const tabs = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'))
  const at = tabs.indexOf(document.activeElement as HTMLButtonElement)
  if (at < 0) return
  let next: number
  switch (e.key) {
    case 'ArrowRight': next = (at + 1) % tabs.length; break
    case 'ArrowLeft': next = (at - 1 + tabs.length) % tabs.length; break
    case 'Home': next = 0; break
    case 'End': next = tabs.length - 1; break
    default: return
  }
  e.preventDefault()
  tabs[next].focus()
  onChange(values[next])
}

// Segmented is the pill filter row (Jobs: All / Running / Queued / Failed).
export function Segmented<T extends string>({options, value, onChange, counts}: {
  options: {value: T, label: string}[], value: T, onChange: (v: T) => void,
  counts?: Partial<Record<T, number>>
}) {
  return (
    <div className="segmented" role="tablist" onKeyDown={e => tabKeyDown(e, options.map(o => o.value), onChange)}>
      {options.map(o => (
        <button key={o.value} role="tab" aria-selected={value === o.value} tabIndex={value === o.value ? 0 : -1}
                className={`segment ${value === o.value ? 'active' : ''}`}
                onClick={() => onChange(o.value)}>
          {o.label}
          {counts?.[o.value] !== undefined && <span className="segment-count">{counts[o.value]}</span>}
        </button>
      ))}
    </div>
  )
}

// Tabs is the underline tab row used in detail panes. Each tab gets the id
// `<idPrefix>-tab-<value>`, and the row points at the panel `<idPrefix>-panel`
// that tabPanelProps renders around the selected content.
export function Tabs<T extends string>({idPrefix, tabs, value, onChange}: {
  idPrefix: string, tabs: {value: T, label: string, icon?: IconName}[], value: T, onChange: (v: T) => void
}) {
  return (
    <div className="tabs" role="tablist" onKeyDown={e => tabKeyDown(e, tabs.map(tab => tab.value), onChange)}>
      {tabs.map(tab => (
        <button key={tab.value} id={`${idPrefix}-tab-${tab.value}`} role="tab"
                aria-selected={value === tab.value} aria-controls={`${idPrefix}-panel`}
                tabIndex={value === tab.value ? 0 : -1}
                className={`tab ${value === tab.value ? 'active' : ''}`}
                onClick={() => onChange(tab.value)}>
          {tab.icon && <Icon name={tab.icon} size={14}/>}{tab.label}
        </button>
      ))}
    </div>
  )
}

// tabPanelProps are the attributes of the element that holds the selected
// tab's content; it is labelled by that tab.
export function tabPanelProps(idPrefix: string, value: string) {
  return {id: `${idPrefix}-panel`, role: 'tabpanel', 'aria-labelledby': `${idPrefix}-tab-${value}`, tabIndex: 0}
}

// Badge is a small pill: neutral by default, toned for status.
// Badge is a short fact. A badge that states a status sets dot, so the status
// is not carried by colour alone (DESIGN.md).
export function Badge({children, tone, dot}: {
  children: ReactNode, tone?: 'accent' | 'success' | 'warning' | 'error' | 'neutral', dot?: boolean
}) {
  return <span className={`badge ${tone ? `badge-${tone}` : ''}`}>
    {dot && <span className="badge-dot" aria-hidden/>}{children}
  </span>
}

export function Empty({icon, title, children}: {icon?: IconName, title: string, children?: ReactNode}) {
  return (
    <div className="empty">
      {icon && <span className="empty-icon"><Icon name={icon} size={20}/></span>}
      <div className="empty-title">{title}</div>
      {children && <div className="empty-text">{children}</div>}
    </div>
  )
}

export function Spinner({label}: {label?: string}) {
  return (
    <div className="loading">
      <span className="spinner" aria-hidden/>
      {label && <span>{label}</span>}
    </div>
  )
}

// RefreshButton reloads a view on click and shows what it is doing: disabled
// with a spinner while the reload is in flight, then "Refreshed" or "Failed"
// for a moment before it goes back to idle. onRefresh must return a promise
// that rejects when the reload fails; the view shows the error itself.
export function RefreshButton({label = 'Refresh', onRefresh}: {
  label?: 'Refresh' | 'Reload', onRefresh: () => Promise<unknown>
}) {
  const [state, setState] = useState<'idle' | 'busy' | 'done' | 'failed'>('idle')
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const click = () => {
    window.clearTimeout(timer.current)
    setState('busy')
    // new Promise turns a synchronous throw from onRefresh into a rejection.
    new Promise(resolve => resolve(onRefresh()))
      .then(() => 'done' as const, () => 'failed' as const)
      .then(result => {
        setState(result)
        timer.current = window.setTimeout(() => setState('idle'), 1800)
      })
  }

  const words = label === 'Reload'
    ? {idle: t('common.reload'), busy: t('ui.reloading'), done: t('ui.reloaded')}
    : {idle: t('common.refresh'), busy: t('ui.refreshing'), done: t('ui.refreshed')}
  const shown = {
    idle: {icon: 'refresh', text: words.idle},
    busy: {icon: 'refresh', text: words.busy},
    done: {icon: 'check', text: words.done},
    failed: {icon: 'warning', text: t('ui.failed')},
  } as const
  const current = shown[state]

  return (
    <button className={`btn btn-sm btn-ghost ${state === 'failed' ? 'btn-failed' : ''}`}
            disabled={state === 'busy'} onClick={click} aria-live="polite">
      {state === 'busy'
        ? <span className="spinner" aria-hidden/>
        : <Icon name={current.icon} size={14}/>}
      {current.text}
    </button>
  )
}

// ConfirmRequest is an action to confirm before it runs. Confirmations are
// in-app dialogs, not window.confirm(): WKWebView, which Wails uses on macOS,
// has no confirm panel, so the native call answers "no" and nothing happens.
export type ConfirmRequest = {
  title: string
  message: ReactNode
  action: string
  danger?: boolean
  // primary marks an install action; other confirms use the default role.
  primary?: boolean
  // run may return a promise. The dialog then stays open and busy until it
  // settles, so the action cannot be started a second time while it is in flight.
  run: () => void | Promise<unknown>
}

// useConfirm returns ask(request) to open the confirmation and dialog to
// render alongside the view.
export function useConfirm(): {ask: (request: ConfirmRequest) => void, dialog: ReactNode} {
  const [request, setRequest] = useState<ConfirmRequest | null>(null)
  return {
    ask: setRequest,
    dialog: request && <ConfirmDialog request={request} onClose={() => setRequest(null)}/>,
  }
}

function ConfirmDialog({request, onClose}: {request: ConfirmRequest, onClose: () => void}) {
  const [busy, setBusy] = useState(false)
  const go = () => {
    const result = request.run()
    if (!(result instanceof Promise)) { onClose(); return }
    // The action reports its own failure in the page, so a rejection only ends the wait here.
    setBusy(true)
    result.then(onClose, onClose)
  }
  return (
    <Dialog title={request.title} onClose={onClose} width={440} busy={busy} footer={<>
      {/* A destructive confirm opens on Cancel, so a stray Enter does not run it. */}
      <button className="btn" onClick={onClose} disabled={busy} autoFocus={!!request.danger}>{t('common.cancel')}</button>
      <button className={`btn ${request.danger ? 'btn-danger-tint' : request.primary ? 'btn-primary' : ''}`.trim()}
              onClick={go} disabled={busy} autoFocus={!request.danger}>
        {request.action}
      </button>
    </>}>
      <p className="muted" style={{margin: 0, lineHeight: 1.5}}>{request.message}</p>
    </Dialog>
  )
}

// Dialog is the modal frame: title bar, scrolling body, footer.
// Dialog closes on Escape, the X and a click outside it. While busy (a save in
// flight) every close is ignored, so a typed form is not lost mid-request. After
// an edit inside the body, closing asks first, so typed input is not discarded
// by a stray click. Edits are the change events of the body's fields; values
// filled in by a load do not count.
// focusable lists the elements a keyboard user can reach inside an element.
const focusableSelector = 'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), ' +
  'textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'

// lastClicked is the control the latest click landed on. WKWebView does not
// focus a button when it is clicked, so a dialog opened by a click finds
// document.body as its active element and falls back to this as its opener.
// It is kept past the click because some dialogs open after a request resolves.
let lastClicked: HTMLElement | null = null
document.addEventListener('click', e => {
  lastClicked = (e.target as Element).closest<HTMLElement>(focusableSelector)
}, true)

// DialogCloseContext hands a dialog's guarded close to its footer, so a Cancel
// button asks before it discards edits, as the header X does.
const DialogCloseContext = createContext<(() => void) | null>(null)

// CancelButton is the Cancel of a Dialog footer. It closes through the same
// guard as the header X and Escape.
// HintButton is a row action that can be unavailable for a reason. Unlike a
// disabled button it stays in the tab order, so keyboard and screen reader users
// reach the reason, which is also the tooltip for pointer users.
export function HintButton({disabled, reason, className, onClick, children}:
  {disabled?: boolean, reason?: string, className?: string, onClick: () => void, children: ReactNode}) {
  const id = useId()
  const hint = disabled && reason ? reason : undefined
  // The wrapper keeps the hidden hint inside the button's box; an absolutely
  // positioned hint in the cell would widen the page when the cell is at the edge.
  return <span className="hint-button">
    <button type="button" className={className} aria-disabled={disabled || undefined}
            aria-describedby={hint ? id : undefined} title={hint}
            onClick={() => { if (!disabled) onClick() }}>{children}</button>
    {hint && <span id={id} className="visually-hidden">{hint}</span>}
  </span>
}

export function CancelButton({disabled}: {disabled?: boolean}) {
  const close = useContext(DialogCloseContext)
  return <button className="btn" onClick={() => close?.()} disabled={disabled}>{t('common.cancel')}</button>
}

// dialogStack lists the open dialogs, outermost first. Escape and Tab belong to
// the innermost one only, so a dialog opened from another does not close it too.
const dialogStack: object[] = []

export function Dialog({title, subtitle, onClose, children, footer, width, busy}: {
  title: string, subtitle?: ReactNode, onClose: () => void, children: ReactNode,
  footer?: ReactNode, width?: number, busy?: boolean
}) {
  const [edited, setEdited] = useState(false)
  const [asking, setAsking] = useState(false)
  const [token] = useState(() => ({}))
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    dialogStack.push(token)
    return () => {
      dialogStack.splice(dialogStack.indexOf(token), 1)
    }
  }, [token])
  // resume is the element that had focus when the discard prompt opened. The
  // prompt takes focus, so leaving it puts focus back there.
  const resume = useRef<HTMLElement | null>(null)
  const close = () => {
    // While the discard prompt is up, only its own buttons or Escape act.
    if (busy || asking) return
    if (edited) {
      resume.current = document.activeElement as HTMLElement | null
      setAsking(true)
    } else onClose()
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || dialogStack.at(-1) !== token) return
      if (asking) setAsking(false)
      else close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
  // Focus moves into the dialog when it opens (an autoFocus field keeps it)
  // and goes back to the opener when it closes. The opener is read while
  // rendering: autoFocus runs at commit, before the effects, and would
  // otherwise be taken for the opener. The first field of the body gets focus,
  // not the close button in the title bar.
  const [opener] = useState(() => {
    const active = document.activeElement as HTMLElement | null
    if (active && active !== document.body) return active
    return lastClicked?.isConnected ? lastClicked : null
  })
  const restoreTimer = useRef(0)
  useEffect(() => {
    // StrictMode unmounts and mounts again in development. The return of focus
    // waits a tick, so that first unmount does not hand focus back to the opener
    // and make the remount move it again.
    window.clearTimeout(restoreTimer.current)
    const el = box.current
    if (el && !el.contains(document.activeElement)) {
      const first = el.querySelector('.dialog-body')?.querySelector<HTMLElement>(focusableSelector)
        ?? el.querySelector<HTMLElement>(focusableSelector)
      first?.focus()
    }
    return () => {
      restoreTimer.current = window.setTimeout(() => opener?.focus?.())
    }
  }, [])
  // Leaving the discard prompt, by Keep editing or Escape, returns focus to the
  // field the user was in. The prompt's button is removed by then, so without
  // this focus would fall to the page body.
  useEffect(() => {
    const target = resume.current
    if (asking || !target) return
    resume.current = null
    const el = box.current
    if (el?.contains(target)) target.focus()
    else el?.querySelector('.dialog-body')?.querySelector<HTMLElement>(focusableSelector)?.focus()
  }, [asking])
  // Tab and Shift+Tab stay inside the dialog while it is open.
  useEffect(() => {
    const onTab = (e: KeyboardEvent) => {
      const el = box.current
      if (e.key !== 'Tab' || !el || dialogStack.at(-1) !== token) return
      const items = el.querySelectorAll<HTMLElement>(focusableSelector)
      if (items.length === 0) return
      const first = items[0], last = items[items.length - 1]
      const active = document.activeElement
      const outside = !el.contains(active)
      if (e.shiftKey && (active === first || outside)) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (active === last || outside)) {
        e.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', onTab)
    return () => window.removeEventListener('keydown', onTab)
  }, [])
  return (
    <DialogCloseContext.Provider value={close}>
    <div className="overlay" onMouseDown={e => e.target === e.currentTarget && close()}>
      <div ref={box} className="dialog" style={width ? {width} : undefined} role="dialog" aria-modal aria-label={title}>
        <div className="dialog-head">
          <div>
            <h2>{title}</h2>
            {subtitle && <div className="dialog-sub">{subtitle}</div>}
          </div>
          <button className="icon-btn" onClick={close} disabled={busy} aria-label={t('common.close')}><Icon name="close"/></button>
        </div>
        <div className="dialog-body" onChangeCapture={() => setEdited(true)}>
          {asking && <div className="banner banner-warn" role="alertdialog">
            <span>{t('common.discardMessage')}</span>
            <span className="spacer"/>
            <button className="btn btn-sm" onClick={() => setAsking(false)} autoFocus>{t('common.keepEditing')}</button>
            <button className="btn btn-sm btn-danger-tint" onClick={onClose}>{t('common.discard')}</button>
          </div>}
          {children}
        </div>
        {footer && <div className="dialog-foot">{footer}</div>}
      </div>
    </div>
    </DialogCloseContext.Provider>
  )
}

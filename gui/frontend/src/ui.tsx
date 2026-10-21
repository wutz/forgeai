// Shared UI primitives: icon set, page header, metric tiles, meters,
// segmented filters, tabs, badges, empty state and dialogs. Everything here
// renders from tokens in style.css (gui/DESIGN.md).

import {ReactNode, createContext, useContext, useState} from 'react'
import {createPortal} from 'react-dom'

// HeaderSlot is the node in the app header that PageHeader renders into, so
// every view shows its title and actions in the top bar instead of repeating
// a title band at the top of the content.
export const HeaderSlot = createContext<HTMLElement | null>(null)

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

// PageHeader is the title band of every view. It renders into the app header
// (HeaderSlot) so the top bar carries the title, optional inline meta and the
// actions; views outside a header fall back to a band above the content.
export function PageHeader({eyebrow, title, meta, actions}: {
  eyebrow?: string, title: string, meta?: ReactNode, actions?: ReactNode
}) {
  const slot = useContext(HeaderSlot)
  const content = (
    <>
      <div className="page-head-text">
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <div className="page-head-row">
          <h1 className="page-title">{title}</h1>
          {meta}
        </div>
      </div>
      {actions && <div className="page-head-actions">{actions}</div>}
    </>
  )
  return slot
    ? createPortal(<div className="page-head in-header">{content}</div>, slot)
    : <div className="page-head">{content}</div>
}

export function Card({title, subtitle, actions, children, className = ''}: {
  title?: ReactNode, subtitle?: ReactNode, actions?: ReactNode, children: ReactNode, className?: string
}) {
  return (
    <section className={`card ${className}`}>
      {(title || actions) && (
        <div className="card-head">
          <div>
            {title && <h3 className="card-title">{title}</h3>}
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
           aria-valuemin={0} aria-valuemax={100}>
        <div className={`meter-fill meter-${tone}`} style={{width: `${pct}%`}}/>
      </div>
    </div>
  )
}

// Segmented is the pill filter row (Jobs: All / Running / Queued / Failed).
export function Segmented<T extends string>({options, value, onChange, counts}: {
  options: {value: T, label: string}[], value: T, onChange: (v: T) => void,
  counts?: Partial<Record<T, number>>
}) {
  return (
    <div className="segmented" role="tablist">
      {options.map(o => (
        <button key={o.value} role="tab" aria-selected={value === o.value}
                className={`segment ${value === o.value ? 'active' : ''}`}
                onClick={() => onChange(o.value)}>
          {o.label}
          {counts?.[o.value] !== undefined && <span className="segment-count">{counts[o.value]}</span>}
        </button>
      ))}
    </div>
  )
}

// Tabs is the underline tab row used in detail panes.
export function Tabs<T extends string>({tabs, value, onChange}: {
  tabs: {value: T, label: string, icon?: IconName}[], value: T, onChange: (v: T) => void
}) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map(t => (
        <button key={t.value} role="tab" aria-selected={value === t.value}
                className={`tab ${value === t.value ? 'active' : ''}`}
                onClick={() => onChange(t.value)}>
          {t.icon && <Icon name={t.icon} size={14}/>}{t.label}
        </button>
      ))}
    </div>
  )
}

// Badge is a small pill: neutral by default, toned for status.
export function Badge({children, tone}: {
  children: ReactNode, tone?: 'accent' | 'success' | 'warning' | 'error' | 'neutral'
}) {
  return <span className={`badge ${tone ? `badge-${tone}` : ''}`}>{children}</span>
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

// ConfirmRequest is an action to confirm before it runs. Confirmations are
// in-app dialogs, not window.confirm(): WKWebView, which Wails uses on macOS,
// has no confirm panel, so the native call answers "no" and nothing happens.
export type ConfirmRequest = {
  title: string
  message: ReactNode
  action: string
  danger?: boolean
  run: () => void
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
  const go = () => {
    request.run()
    onClose()
  }
  return (
    <Dialog title={request.title} onClose={onClose} width={420} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className={`btn ${request.danger ? 'btn-danger-tint' : 'btn-primary'}`} onClick={go} autoFocus>
        {request.action}
      </button>
    </>}>
      <p className="muted" style={{margin: 0, lineHeight: 1.5}}>{request.message}</p>
    </Dialog>
  )
}

// Dialog is the modal frame: title bar, scrolling body, footer.
export function Dialog({title, subtitle, onClose, children, footer, width}: {
  title: string, subtitle?: ReactNode, onClose: () => void, children: ReactNode,
  footer?: ReactNode, width?: number
}) {
  return (
    <div className="overlay" onMouseDown={e => e.target === e.currentTarget && onClose()}>
      <div className="dialog" style={width ? {width} : undefined} role="dialog" aria-modal>
        <div className="dialog-head">
          <div>
            <h2>{title}</h2>
            {subtitle && <div className="dialog-sub">{subtitle}</div>}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label="Close"><Icon name="close"/></button>
        </div>
        <div className="dialog-body">{children}</div>
        {footer && <div className="dialog-foot">{footer}</div>}
      </div>
    </div>
  )
}

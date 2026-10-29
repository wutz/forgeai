import {CSSProperties, KeyboardEvent, useEffect, useLayoutEffect, useRef, useState} from 'react'
import {createPortal} from 'react-dom'
import {ListImages, Registry} from '../wailsjs/go/app/App'
import {fmtDate} from './util'
import {Icon} from './ui'
import {t} from './i18n'

// Choice is one row of the menu. "typed" is the address being typed when it is
// not a suggestion, so Enter can use it as it is.
type Choice = {
  kind: 'typed' | 'project' | 'platform'
  address: string
  sizeMiB?: number
  pushedAt?: string
}

// FLIP_BELOW is the room under the field under which the menu opens upwards.
const FLIP_BELOW = 200
const MENU_GAP = 4
// MENU_MAX matches the .combo-menu max-height; MENU_EDGE keeps the menu off the window edge.
const MENU_MAX = 300
const MENU_EDGE = 8

// splitAddress separates the registry path from the image name, so the menu
// can show the name first and the path dimmed.
function splitAddress(address: string): [string, string] {
  const i = address.lastIndexOf('/') + 1
  return [address.slice(0, i), address.slice(i)]
}

// ImageField is the image input of the job and sandbox forms and their
// templates: a combobox over the tags pushed to the project registry (the
// Images view) and the platform's offered images. Any other address can be
// typed and picked with Enter. The menu is portalled to the body because the
// dialog body scrolls and clips.
export function ImageField({id, value, onChange, offered, example, required}: {
  id: string, value: string, onChange: (image: string) => void,
  // offered is null when the platform config lists no images (Go nil slice).
  offered?: string[] | null,
  // example is an image name and tag, e.g. "agent:latest"; the placeholder puts
  // it under the project's own registry path, so the user sees the address
  // they would push to instead of a made-up one.
  example?: string, required?: boolean,
}) {
  // registryPath is the project's push address (registry host and Harbor
  // project, which is named after the namespace). It stays empty without a
  // registry, and the placeholder then falls back to a generic address.
  const [registryPath, setRegistryPath] = useState('')
  const [open, setOpen] = useState(false)
  // query is what was typed since the menu opened; null lists every image.
  const [query, setQuery] = useState<string | null>(null)
  // active is the highlighted row; null highlights the current value.
  const [active, setActive] = useState<number | null>(null)
  // pushed stays null until the menu first opens, so the dialog makes no
  // registry calls it does not need.
  const [pushed, setPushed] = useState<Choice[] | null>(null)
  const [pushedFailed, setPushedFailed] = useState(false)
  // registryOff is set when the cluster has no project registry, which is not a failure.
  const [registryOff, setRegistryOff] = useState(false)
  const [pos, setPos] = useState<CSSProperties>({})
  const inputRef = useRef<HTMLInputElement>(null)
  const wrap = useRef<HTMLDivElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const requested = useRef(false)

  useEffect(() => {
    if (!example) return
    let live = true
    Registry().then(reg => { if (live && reg.enabled) setRegistryPath(reg.url) }, () => {})
    return () => { live = false }
  }, [example])

  // Registry and ListImages fail for view-only members (no robot credentials);
  // the menu then offers only the platform images. A failure is retried on the
  // next open.
  const loadPushed = () => {
    if (requested.current) return
    requested.current = true
    Promise.all([Registry(), ListImages()]).then(([reg, repos]) => {
      setRegistryOff(!reg.enabled)
      if (!reg.enabled) return []
      const out: Choice[] = []
      for (const r of repos) for (const img of r.images ?? []) for (const tag of img.tags ?? []) {
        if (!tag) continue
        out.push({kind: 'project', address: `${reg.url}/${r.name}:${tag}`, sizeMiB: img.sizeMiB, pushedAt: img.pushedAt})
      }
      return out.sort((a, b) => (b.pushedAt ?? '').localeCompare(a.pushedAt ?? ''))
    }).then(list => {
      setPushed(list)
      setPushedFailed(false)
    }, () => {
      requested.current = false
      setPushed([])
      setPushedFailed(true)
    })
  }

  const typed = (query ?? '').trim()
  const q = typed.toLowerCase()
  const pushedList = pushed ?? []
  // A platform image that is also pushed to the project is listed once, as a project image.
  const platformList: Choice[] = (offered ?? [])
    .filter(a => !pushedList.some(p => p.address === a))
    .map(address => ({kind: 'platform', address}))
  const projects = pushedList.filter(c => c.address.toLowerCase().includes(q))
  const platforms = platformList.filter(c => c.address.toLowerCase().includes(q))
  const exists = [...pushedList, ...platformList].some(c => c.address === typed)
  const typedChoice: Choice | null = typed && !exists ? {kind: 'typed', address: typed} : null
  // rows is the keyboard order, which is also the visual order.
  const rows: Choice[] = [...(typedChoice ? [typedChoice] : []), ...projects, ...platforms]
  const current = Math.min(active ?? Math.max(0, rows.findIndex(r => r.address === value)), rows.length - 1)

  // place anchors the menu under the field, or above it when there is less room
  // below, and caps its height to the room it gets so it stays inside the window.
  const place = () => {
    const r = wrap.current?.getBoundingClientRect()
    if (!r) return
    const below = Math.max(0, window.innerHeight - r.bottom - MENU_GAP - MENU_EDGE)
    const above = Math.max(0, r.top - MENU_GAP - MENU_EDGE)
    const box = {left: r.left, width: r.width}
    setPos(below < FLIP_BELOW && above > below
      ? {...box, bottom: window.innerHeight - r.top + MENU_GAP, maxHeight: Math.min(MENU_MAX, above)}
      : {...box, top: r.bottom + MENU_GAP, maxHeight: Math.min(MENU_MAX, below)})
  }

  useLayoutEffect(() => {
    if (open) place()
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (!wrap.current?.contains(t) && !menu.current?.contains(t)) setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open])

  useEffect(() => {
    if (open) document.getElementById(`${id}-opt-${current}`)?.scrollIntoView({block: 'nearest'})
  }, [open, current, id])

  const openMenu = () => {
    loadPushed()
    setQuery(null)
    setActive(null)
    setOpen(true)
  }
  const type = (text: string) => {
    onChange(text)
    loadPushed()
    setQuery(text)
    setActive(null)
    setOpen(true)
  }
  // pick keeps focus in the field, so a mouse pick does not drop focus to the body.
  const pick = (address: string) => {
    onChange(address)
    setOpen(false)
    setQuery(null)
    inputRef.current?.focus()
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (!open) return openMenu()
      if (rows.length) setActive(Math.min(Math.max(current + (e.key === 'ArrowDown' ? 1 : -1), 0), rows.length - 1))
    } else if (e.key === 'Enter' && open && rows[current]) {
      // Enter picks the row; with the menu closed it submits the form as usual.
      e.preventDefault()
      pick(rows[current].address)
    } else if (e.key === 'Escape' && open) {
      e.preventDefault()
      e.stopPropagation()
      setOpen(false)
    } else if (e.key === 'Tab') {
      setOpen(false)
    }
  }

  const option = (c: Choice) => {
    const i = rows.indexOf(c)
    const [prefix, name] = splitAddress(c.address)
    return (
      <div key={`${c.kind}-${c.address}`} id={`${id}-opt-${i}`} role="option" aria-selected={c.address === value}
           className={`combo-option${i === current ? ' active' : ''}`}
           onMouseEnter={() => setActive(i)} onClick={() => pick(c.address)}>
        {c.kind === 'typed' ? <>
          <Icon name="plus" size={13}/>
          <span className="combo-label">{t('imagePicker.use')}</span>
          <span className="combo-address mono"><span className="combo-prefix">{c.address}</span></span>
        </> : <>
          <span className="combo-address mono" title={c.address}>
            <span className="combo-prefix">{prefix}</span><span className="combo-name">{name}</span>
          </span>
          {c.sizeMiB ? <span className="combo-meta">{c.sizeMiB} MiB</span> : null}
          {c.pushedAt ? <span className="combo-meta">{fmtDate(c.pushedAt)}</span> : null}
        </>}
        {c.address === value && <Icon name="check" size={13}/>}
      </div>
    )
  }

  const projectNote = pushed === null ? t('imagePicker.loading')
    : registryOff ? t('images.notConfiguredHint')
    : pushedFailed ? t('imagePicker.unavailable')
    : t('imagePicker.none')

  const menuNode = open && createPortal(
    <div ref={menu} id={`${id}-menu`} className="combo-menu" role="listbox" style={pos}>
      {typedChoice && option(typedChoice)}
      {(projects.length > 0 || !q) && <div role="group" aria-label={t('imagePicker.projects')}>
        <div className="combo-group" aria-hidden="true">{t('imagePicker.projects')}</div>
        {projects.length > 0 ? projects.map(option) : <div className="combo-note">{projectNote}</div>}
      </div>}
      {platforms.length > 0 && <div role="group" aria-label={t('imagePicker.platform')}>
        <div className="combo-group" aria-hidden="true">{t('imagePicker.platform')}</div>
        {platforms.map(option)}
      </div>}
      <div className="combo-foot">{t('imagePicker.keys')}</div>
    </div>,
    document.body,
  )

  return (
    <div ref={wrap} className="combo">
      <input ref={inputRef} id={id} className="input mono" value={value}
             role="combobox" aria-expanded={open} aria-controls={`${id}-menu`} aria-autocomplete="list"
             aria-activedescendant={open && rows[current] ? `${id}-opt-${current}` : undefined}
             autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false}
             onChange={e => type(e.target.value)} onKeyDown={onKeyDown}
             onClick={() => { if (!open) openMenu() }}
             placeholder={example ? `${registryPath || 'registry.example.com/project'}/${example}` : undefined}
             required={required}/>
      <button type="button" className="icon-btn combo-toggle" tabIndex={-1} aria-label={t('imagePicker.show')}
              aria-expanded={open}
              onClick={() => {
                if (open) setOpen(false)
                else {
                  openMenu()
                  inputRef.current?.focus()
                }
              }}>
        <span className={`combo-caret${open ? ' open' : ''}`}><Icon name="chevron" size={13}/></span>
      </button>
      {menuNode}
    </div>
  )
}

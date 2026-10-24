import {FocusEvent as ReactFocusEvent, KeyboardEvent as ReactKeyboardEvent, useCallback, useEffect, useRef, useState, useSyncExternalStore} from 'react'
import {LocaleChoice, getLocaleChoice, setLocaleChoice, subscribeLocale, t} from './i18n'
import {ThemeChoice, getThemeChoice, setThemeChoice, subscribeTheme} from './theme'
import {Icon, IconName} from './ui'
import {Update, VersionMenu} from './Update'

type Option<T extends string> = {value: T, label: string, icon?: IconName}

// PrefMenu is a compact control: a button showing the current choice (its icon,
// or its label when there is none) that opens a menu of the options. A pick
// closes it, as do Escape, Tab out of the menu and a click elsewhere. Closing
// from inside the menu returns focus to the button, so focus is never lost.
function PrefMenu<T extends string>({title, value, options, onChange}: {
  title: string, value: T, options: Option<T>[], onChange: (v: T) => void
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const popover = useRef<HTMLDivElement>(null)
  const current = options.find(o => o.value === value) ?? options[0]

  // close hands focus to the button before the menu unmounts, so the focused
  // item is never removed from the page while it has focus.
  const close = useCallback(() => {
    trigger.current?.focus()
    setOpen(false)
  }, [])

  // Opening moves focus to the current choice, as a menu does.
  useEffect(() => {
    if (open) popover.current?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus()
  }, [open])

  useEffect(() => {
    if (!open) return
    const onPointer = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open, close])

  // Arrow keys, Home and End move between the options.
  const moveWithin = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]')]
    const at = items.findIndex(item => item === document.activeElement)
    let next = -1
    if (e.key === 'ArrowDown') next = (at + 1) % items.length
    else if (e.key === 'ArrowUp') next = (at - 1 + items.length) % items.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = items.length - 1
    if (next >= 0) {
      e.preventDefault()
      items[next].focus()
    }
  }

  // Focus leaving the menu (Tab, or a click on something else) closes it.
  const leave = (e: ReactFocusEvent<HTMLDivElement>) => {
    if (open && !root.current?.contains(e.relatedTarget as Node)) setOpen(false)
  }

  return (
    <div className="pref-menu" ref={root} onBlur={leave}>
      <button ref={trigger} className="pref-trigger" aria-haspopup="menu" aria-expanded={open}
              aria-label={title} title={title} onClick={() => setOpen(v => !v)}>
        {current.icon ? <Icon name={current.icon} size={14}/> : <span>{current.label}</span>}
        <span className="pref-caret" aria-hidden><Icon name="chevron" size={11}/></span>
      </button>
      {open && (
        <div ref={popover} className="pref-popover" role="menu" aria-label={title} onKeyDown={moveWithin}>
          {options.map(o => (
            <button key={o.value} role="menuitemradio" aria-checked={o.value === value}
                    className={`pref-item ${o.value === value ? 'active' : ''}`}
                    onClick={() => {
                      onChange(o.value)
                      close()
                    }}>
              {o.icon && <Icon name={o.icon} size={14}/>}
              <span>{o.label}</span>
              <span className="pref-check" aria-hidden>{o.value === value && <Icon name="check" size={13}/>}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// Preferences is the theme and language menus at the foot of the sidebar, and
// the version menu after them. Each preference is one button showing the
// current choice; "Auto" (follow the system) is the default for both, and the
// choice is saved.
export function Preferences({update}: {update: Update}) {
  const theme = useSyncExternalStore(subscribeTheme, getThemeChoice)
  const language = useSyncExternalStore(subscribeLocale, getLocaleChoice)

  const themes: Option<ThemeChoice>[] = [
    {value: 'system', label: t('prefs.themeSystem'), icon: 'monitor'},
    {value: 'light', label: t('prefs.themeLight'), icon: 'sun'},
    {value: 'dark', label: t('prefs.themeDark'), icon: 'moon'},
  ]
  const languages: Option<LocaleChoice>[] = [
    {value: 'system', label: t('prefs.langSystem')},
    {value: 'en', label: 'English'},
    {value: 'zh', label: '中文'},
  ]

  return (
    <div className="sidebar-prefs">
      <PrefMenu title={t('prefs.themeGroup')} value={theme} options={themes} onChange={setThemeChoice}/>
      <PrefMenu title={t('prefs.langGroup')} value={language} options={languages} onChange={setLocaleChoice}/>
      <VersionMenu update={update}/>
    </div>
  )
}

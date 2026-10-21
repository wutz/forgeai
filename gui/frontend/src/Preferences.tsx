import {useEffect, useRef, useState, useSyncExternalStore} from 'react'
import {LocaleChoice, getLocaleChoice, setLocaleChoice, subscribeLocale, t} from './i18n'
import {ThemeChoice, getThemeChoice, setThemeChoice, subscribeTheme} from './theme'
import {Icon, IconName} from './ui'

type Option<T extends string> = {value: T, label: string, icon?: IconName}

// PrefMenu is a compact control: a button showing the current choice (its icon,
// or its label when there is none) that opens a menu of the options. A pick
// closes it, as do Escape and a click elsewhere.
function PrefMenu<T extends string>({title, value, options, onChange}: {
  title: string, value: T, options: Option<T>[], onChange: (v: T) => void
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const current = options.find(o => o.value === value) ?? options[0]

  useEffect(() => {
    if (!open) return
    const onPointer = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div className="pref-menu" ref={root}>
      <button className="pref-trigger" aria-haspopup="menu" aria-expanded={open}
              aria-label={title} title={title} onClick={() => setOpen(v => !v)}>
        {current.icon ? <Icon name={current.icon} size={14}/> : <span>{current.label}</span>}
        <span className="pref-caret" aria-hidden><Icon name="chevron" size={11}/></span>
      </button>
      {open && (
        <div className="pref-popover" role="menu" aria-label={title}>
          {options.map(o => (
            <button key={o.value} role="menuitemradio" aria-checked={o.value === value}
                    className={`pref-item ${o.value === value ? 'active' : ''}`}
                    onClick={() => {
                      onChange(o.value)
                      setOpen(false)
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

// Preferences is the theme and language menus at the foot of the sidebar. Each
// is one button showing the current choice; "Auto" (follow the system) is the
// default for both, and the choice is saved.
export function Preferences() {
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
    </div>
  )
}

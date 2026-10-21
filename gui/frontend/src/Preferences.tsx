import {ReactNode, useSyncExternalStore} from 'react'
import {LocaleChoice, getLocaleChoice, setLocaleChoice, subscribeLocale, t} from './i18n'
import {ThemeChoice, getThemeChoice, setThemeChoice, subscribeTheme} from './theme'
import {Icon, Segmented} from './ui'

// Preferences is the theme and language switch at the foot of the sidebar.
// Each is a full-width segmented control; the choice is saved and "Auto"
// (system) is the default.
export function Preferences() {
  const theme = useSyncExternalStore(subscribeTheme, getThemeChoice)
  const language = useSyncExternalStore(subscribeLocale, getLocaleChoice)

  const themes: {value: ThemeChoice, label: ReactNode, title: string}[] = [
    {value: 'system', label: <Icon name="monitor" size={14}/>, title: t('prefs.themeSystem')},
    {value: 'light', label: <Icon name="sun" size={14}/>, title: t('prefs.themeLight')},
    {value: 'dark', label: <Icon name="moon" size={14}/>, title: t('prefs.themeDark')},
  ]
  const languages: {value: LocaleChoice, label: string, title: string}[] = [
    {value: 'system', label: t('prefs.langSystem'), title: t('prefs.langSystemTitle')},
    {value: 'en', label: 'EN', title: 'English'},
    {value: 'zh', label: '中文', title: '中文'},
  ]

  return (
    <div className="sidebar-prefs">
      <div className="nav-label">{t('prefs.themeGroup')}</div>
      <Segmented label={t('prefs.themeGroup')} options={themes} value={theme} onChange={setThemeChoice}/>
      <div className="nav-label">{t('prefs.langGroup')}</div>
      <Segmented label={t('prefs.langGroup')} options={languages} value={language} onChange={setLocaleChoice}/>
    </div>
  )
}

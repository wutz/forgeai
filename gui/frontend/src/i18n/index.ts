// Localization: English and Simplified Chinese. By default the language follows
// the system: the first preferred language the app has a catalog for wins, and
// anything else falls back to English. The user can pin English or Chinese in
// the top bar; the choice is kept in localStorage. Messages live in ./messages,
// one file per screen group, each exporting `en` and a `zh` typed to the same
// keys, so a missing translation is a type error.

import {en as commonEn, zh as commonZh} from './messages/common'
import {en as appEn, zh as appZh} from './messages/app'
import {en as jobsEn, zh as jobsZh} from './messages/jobs'
import {en as sandboxesEn, zh as sandboxesZh} from './messages/sandboxes'
import {en as storageEn, zh as storageZh} from './messages/storage'
import {en as templatesEn, zh as templatesZh} from './messages/templates'
import {en as clusterEn, zh as clusterZh} from './messages/cluster'

export type Locale = 'en' | 'zh'

const en = {...commonEn, ...appEn, ...jobsEn, ...sandboxesEn, ...storageEn, ...templatesEn, ...clusterEn}

export type MessageKey = keyof typeof en

const zh: Record<MessageKey, string> = {
  ...commonZh, ...appZh, ...jobsZh, ...sandboxesZh, ...storageZh, ...templatesZh, ...clusterZh,
}

const catalogs: Record<Locale, Record<MessageKey, string>> = {en, zh}

// detectLocale returns the language of the first preferred entry the app has a
// catalog for. Traditional Chinese regions also get the Simplified text, which
// reads better than English for those readers.
export function detectLocale(languages: readonly string[] = navigator.languages): Locale {
  for (const language of languages) {
    const base = language.toLowerCase().split('-')[0]
    if (base === 'zh') return 'zh'
    if (base === 'en') return 'en'
  }
  return 'en'
}

// LocaleChoice is what the user picked: follow the system, or a fixed language.
export type LocaleChoice = 'system' | Locale

const STORAGE_KEY = 'forgeai.locale'

function readChoice(): LocaleChoice {
  const saved = localStorage.getItem(STORAGE_KEY)
  return saved === 'en' || saved === 'zh' ? saved : 'system'
}

let choice: LocaleChoice = readChoice()
let current: Locale = choice === 'system' ? detectLocale() : choice
const listeners = new Set<() => void>()

// applyLocale records the language on <html> for assistive tech and hyphenation.
function applyLocale() {
  document.documentElement.lang = current
}
applyLocale()

// getLocale is the language in use right now; t reads it on every call.
export function getLocale(): Locale {
  return current
}

export function getLocaleChoice(): LocaleChoice {
  return choice
}

// setLocaleChoice switches the language, saves the choice and tells the
// subscribers, so the open views render again in the new language.
export function setLocaleChoice(next: LocaleChoice) {
  choice = next
  if (next === 'system') localStorage.removeItem(STORAGE_KEY)
  else localStorage.setItem(STORAGE_KEY, next)
  current = next === 'system' ? detectLocale() : next
  applyLocale()
  listeners.forEach(fn => fn())
}

// subscribeLocale calls fn after each language change; it returns the
// unsubscribe function, the shape useSyncExternalStore expects.
export function subscribeLocale(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

// t returns the message for key in the current language. {name} placeholders
// are filled from vars; a placeholder without a value is left as it is. A key
// with no text in the language falls back to English, then to the key itself,
// so a missing message is never shown blank.
export function t(key: MessageKey, vars?: Record<string, string | number>): string {
  const text = catalogs[current][key] ?? en[key] ?? key
  if (!vars) return text
  return text.replace(/\{(\w+)\}/g, (match, name: string) => (name in vars ? String(vars[name]) : match))
}

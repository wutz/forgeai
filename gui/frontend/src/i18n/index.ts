// Localization: English and Simplified Chinese. The language follows the
// system: the first preferred language the app has a catalog for wins, and
// anything else falls back to English. Messages live in ./messages, one file
// per screen group, each exporting `en` and a `zh` typed to the same keys, so a
// missing translation is a type error.

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

export const locale: Locale = detectLocale()

// t returns the message for key in the system language. {name} placeholders
// are filled from vars; a placeholder without a value is left as it is.
export function t(key: MessageKey, vars?: Record<string, string | number>): string {
  const text = catalogs[locale][key]
  if (!vars) return text
  return text.replace(/\{(\w+)\}/g, (match, name: string) => (name in vars ? String(vars[name]) : match))
}

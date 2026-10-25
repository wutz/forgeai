import {app} from '../wailsjs/go/models'

// The template sources the user added on this machine, kept in localStorage as
// a list of locations. The platform's own source is not in this list: the
// platform config names it.
const STORAGE_KEY = 'forgeai.templateSources'

// loadTemplateSources reads the saved locations. A damaged entry counts as none.
export function loadTemplateSources(): string[] {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')
    return Array.isArray(saved) ? saved.filter((s): s is string => typeof s === 'string') : []
  } catch {
    return []
  }
}

export function saveTemplateSources(sources: string[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(sources))
}

// sameSource compares two locations the way the backend does: a trailing slash
// names the same source.
export function sameSource(a: string, b: string): boolean {
  return a.trim().replace(/\/+$/, '') === b.trim().replace(/\/+$/, '')
}

// catalogPick is the template select's value for a catalog template. The source
// location and the template name are JSON, so any location survives the round trip.
export function catalogPick(source: string, name: string): string {
  return `catalog:${JSON.stringify([source, name])}`
}

// parseCatalogPick reads back the location and the name a catalogPick holds.
export function parseCatalogPick(value: string): [string, string] {
  return JSON.parse(value.slice('catalog:'.length))
}

export type CatalogGroup = {source: app.CatalogSource, templates: app.CatalogTemplate[]}

// catalogGroups keeps, per source, the usable templates of one kind. A source
// with none is left out of the picker.
export function catalogGroups(sources: app.CatalogSource[], kind: string): CatalogGroup[] {
  return sources
    .map(source => ({source, templates: (source.templates || []).filter(t => t.kind === kind && !t.error)}))
    .filter(g => g.templates.length > 0)
}

// pickedCatalogTemplate finds the template a select value names, if it is a catalog one.
export function pickedCatalogTemplate(groups: CatalogGroup[], value: string) {
  for (const g of groups) {
    for (const template of g.templates) {
      if (catalogPick(g.source.location, template.name) === value) return {source: g.source, template}
    }
  }
  return undefined
}

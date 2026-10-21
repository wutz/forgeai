// Theme follows the system appearance unless the user pins light or dark in the
// top bar. The data-theme attribute on <html> is "dark" or "light", and the
// colour tokens in style.css and redesign.css key off it. index.html sets it
// before the first paint (honouring the saved choice); this keeps it in step
// when the system switches appearance while the app is open.

// ThemeChoice is what the user picked: follow the system, or a fixed theme.
export type ThemeChoice = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'forgeai.theme'
const dark = window.matchMedia('(prefers-color-scheme: dark)')
const listeners = new Set<() => void>()

function readChoice(): ThemeChoice {
  const saved = localStorage.getItem(STORAGE_KEY)
  return saved === 'light' || saved === 'dark' ? saved : 'system'
}

let choice: ThemeChoice = readChoice()

function resolve(): 'dark' | 'light' {
  if (choice === 'system') return dark.matches ? 'dark' : 'light'
  return choice
}

function applyTheme() {
  document.documentElement.dataset.theme = resolve()
}

export function watchTheme() {
  applyTheme()
  dark.addEventListener('change', applyTheme)
}

export function getThemeChoice(): ThemeChoice {
  return choice
}

// setThemeChoice applies and saves the theme, and tells the subscribers so the
// toggle in the top bar shows the new state.
export function setThemeChoice(next: ThemeChoice) {
  choice = next
  if (next === 'system') localStorage.removeItem(STORAGE_KEY)
  else localStorage.setItem(STORAGE_KEY, next)
  applyTheme()
  listeners.forEach(fn => fn())
}

// subscribeTheme calls fn after each change of choice; it returns the
// unsubscribe function, the shape useSyncExternalStore expects.
export function subscribeTheme(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

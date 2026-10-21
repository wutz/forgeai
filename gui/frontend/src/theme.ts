// Theme follows the system appearance. The data-theme attribute on <html> is
// "dark" or "light", and the colour tokens in style.css and redesign.css key
// off it. index.html sets it before the first paint; this keeps it in step
// when the system switches appearance while the app is open.
const dark = window.matchMedia('(prefers-color-scheme: dark)')

function applyTheme() {
  document.documentElement.dataset.theme = dark.matches ? 'dark' : 'light'
}

export function watchTheme() {
  applyTheme()
  dark.addEventListener('change', applyTheme)
}

// Client-side preferences. Keys must match the inline script in layouts/Base.astro, which applies them before paint.

type Theme = 'system' | 'light' | 'dark';

const themeKey = 'forgeai-theme';
const langKey = 'forgeai-lang';

// localStorage can throw in private modes or when storage is disabled; the site still works without it.
function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // Ignore: the preference simply will not persist.
  }
}

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  // No attribute means "follow the system", which the CSS light-dark() values handle.
  if (theme === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', theme);

  document.querySelectorAll<HTMLButtonElement>('[data-theme-option]').forEach((button) => {
    button.setAttribute('aria-pressed', String(button.dataset.themeOption === theme));
  });
}

const stored = read(themeKey);
applyTheme(stored === 'light' || stored === 'dark' ? stored : 'system');

document.querySelectorAll<HTMLButtonElement>('[data-theme-option]').forEach((button) => {
  button.addEventListener('click', () => {
    const theme = button.dataset.themeOption as Theme;
    write(themeKey, theme === 'system' ? null : theme);
    applyTheme(theme);
  });
});

// Choosing a language stores it, so the root page stops auto-detecting. The link then navigates normally.
document.querySelectorAll<HTMLAnchorElement>('[data-lang-option]').forEach((link) => {
  link.addEventListener('click', () => {
    write('forgeai-lang', link.dataset.langOption ?? null);
  });
});

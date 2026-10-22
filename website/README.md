# forgeai website

The official website of forgeai, at https://forgeai.wutz.dev. It is a static [Astro](https://astro.build) site, deployed to Cloudflare as a Worker with static assets. The Worker also serves the desktop console installers (see [Desktop console downloads](#desktop-console-downloads)). The design system is in [DESIGN.md](DESIGN.md).

| Path | Description |
|---|---|
| `src/pages/` | `index.astro` (English landing page), `zh/index.astro` (Chinese landing page), `404.astro` (both languages) |
| `src/components/` | `Home.astro` (landing page sections, shared by both languages), `SiteHeader.astro` (brand and navigation) and `Preferences.astro` (theme and language controls, shown in the footer) |
| `src/i18n/messages.ts` | All copy, in English and Chinese. `zh` is typed against `en`, so an editor flags a missing key |
| `src/layouts/Base.astro` | HTML shell, metadata, hreflang links, footer, and the inline script that applies the saved theme and language before paint |
| `src/scripts/preferences.ts` | Theme and language controls in the browser |
| `src/styles/global.css` | Design tokens and styles (mirrors `DESIGN.md`), light and dark |
| `worker/index.ts` | Worker that proxies `/download/gui/*` to the latest GitHub release |
| `public/` | Static files served as-is (favicon, robots.txt) |
| `wrangler.jsonc` | Worker config: entry `worker/index.ts`, static assets from `dist/` bound as `ASSETS`, only `/download/*` runs the Worker first, custom domain `forgeai.wutz.dev` |

## Languages and themes

- **Languages.** English is served at `/` and Chinese at `/zh/`. A visitor whose browser prefers Chinese is sent from `/` to `/zh/`, unless they have chosen a language. The footer's language link stores the choice in `localStorage` (`forgeai-lang`).
- **Themes.** The page follows the system light or dark setting by default. The footer's System / Light / Dark control overrides it and stores the choice (`forgeai-theme`).

## Desktop console downloads

The installers are attached to the GitHub release that the `v*` tag creates ([gui-release.yml](../.github/workflows/gui-release.yml)). Some visitors cannot reach github.com, so the site serves them instead:

| URL | Serves |
|---|---|
| `/download/gui/macos` | `forgeai-<version>-macos-universal.dmg` |
| `/download/gui/windows` | `forgeai-<version>-windows-amd64-setup.exe` |
| `/download/gui/windows-portable` | `forgeai-<version>-windows-amd64-portable.exe` |
| `/download/gui/linux-deb` | `forgeai-<version>-linux-amd64.deb` |
| `/download/gui/linux-tar` | `forgeai-<version>-linux-amd64.tar.gz` |
| `/download/gui/checksums` | `SHA256SUMS.txt` |

The Worker reads the newest release tag from `github.com/wutz/forgeai/releases/latest`, caches it for five minutes per isolate, and streams the asset from GitHub. The file names are set by [hack/gui-package.sh](../hack/gui-package.sh); if they change, change the `files` map in `worker/index.ts` too.

To try it locally, build first and then run `npx wrangler dev`. It listens on http://localhost:8787 and serves `/download/gui/*` from the latest release.

## Develop

```sh
npm install
npm run dev       # http://localhost:4321
```

## Build and deploy

```sh
npm run build     # writes dist/
npm run deploy    # builds, then runs wrangler deploy
```

Deploying needs a Cloudflare account that owns the `wutz.dev` zone and a login with `npx wrangler login` (or `CLOUDFLARE_API_TOKEN`). The custom domain route is declared in `wrangler.jsonc`; Cloudflare creates its DNS record and certificate on the first deploy.

Keep the copy in step with the repository: the README is the source for the object model, and the feature descriptions come from it.


# forgeai website

The official website of forgeai, at https://forgeai.wutz.dev. It is a static [Astro](https://astro.build) site, deployed to Cloudflare as a Worker with static assets. The design system is in [DESIGN.md](DESIGN.md).

| Path | Description |
|---|---|
| `src/pages/` | `index.astro` (English landing page), `zh/index.astro` (Chinese landing page), `404.astro` (both languages) |
| `src/components/` | `Home.astro` (landing page sections, shared by both languages), `SiteHeader.astro` (brand and navigation) and `Preferences.astro` (theme and language controls, shown in the footer) |
| `src/i18n/messages.ts` | All copy, in English and Chinese. `zh` is typed against `en`, so an editor flags a missing key |
| `src/layouts/Base.astro` | HTML shell, metadata, hreflang links, footer, and the inline script that applies the saved theme and language before paint |
| `src/scripts/preferences.ts` | Theme and language controls in the browser |
| `src/styles/global.css` | Design tokens and styles (mirrors `DESIGN.md`), light and dark |
| `public/` | Static files served as-is (favicon, robots.txt) |
| `wrangler.jsonc` | Worker config: static assets from `dist/`, custom domain `forgeai.wutz.dev` |

## Languages and themes

- **Languages.** English is served at `/` and Chinese at `/zh/`. A visitor whose browser prefers Chinese is sent from `/` to `/zh/`, unless they have chosen a language. The footer's language link stores the choice in `localStorage` (`forgeai-lang`).
- **Themes.** The page follows the system light or dark setting by default. The footer's System / Light / Dark control overrides it and stores the choice (`forgeai-theme`).

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

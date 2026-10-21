# forgeai website

The official website of forgeai, at https://forgeai.wutz.dev. It is a static [Astro](https://astro.build) site, deployed to Cloudflare as a Worker with static assets. The design system is in [DESIGN.md](DESIGN.md).

| Path | Description |
|---|---|
| `src/pages/` | `index.astro` (landing page) and `404.astro` |
| `src/layouts/Base.astro` | HTML shell, metadata, header and footer |
| `src/styles/global.css` | Design tokens and styles (mirrors `DESIGN.md`) |
| `public/` | Static files served as-is (favicon, robots.txt) |
| `wrangler.jsonc` | Worker config: static assets from `dist/`, custom domain `forgeai.wutz.dev` |

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

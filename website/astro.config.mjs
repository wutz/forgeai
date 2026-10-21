// @ts-check
import { defineConfig } from 'astro/config';

// Static output: `astro build` writes ./dist, which wrangler.jsonc serves as Worker static assets.
export default defineConfig({
  site: 'https://forgeai.wutz.dev',
  output: 'static',
  trailingSlash: 'ignore',
});

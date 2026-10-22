// Serves the desktop console installers from the newest GitHub release. Some visitors cannot reach
// github.com, so the Worker fetches each file itself and streams it to them. wrangler.jsonc routes only
// /download/* here; every other path is served from the static Astro build through ASSETS.

const REPO = 'wutz/forgeai';

// Keys are the URL segment after /download/gui/. File names come from hack/gui-package.sh and the
// release workflow; keep them in step with those.
const files: Record<string, (version: string) => string> = {
  macos: (version) => `forgeai-${version}-macos-universal.dmg`,
  windows: (version) => `forgeai-${version}-windows-amd64-setup.exe`,
  'windows-portable': (version) => `forgeai-${version}-windows-amd64-portable.exe`,
  'linux-deb': (version) => `forgeai-${version}-linux-amd64.deb`,
  'linux-tar': (version) => `forgeai-${version}-linux-amd64.tar.gz`,
  checksums: () => 'SHA256SUMS.txt',
};

// The newest tag changes only when a release is published, so each isolate keeps it for a few minutes.
const LATEST_TTL_MS = 5 * 60 * 1000;
let latest: { tag: string; expires: number } | undefined;

interface Env {
  ASSETS: { fetch(request: Request): Promise<Response> };
}

async function latestTag(): Promise<string> {
  if (latest && latest.expires > Date.now()) return latest.tag;

  // github.com answers /releases/latest with a redirect to the newest published, non-prerelease tag.
  const response = await fetch(`https://github.com/${REPO}/releases/latest`, { redirect: 'manual' });
  const tag = /\/releases\/tag\/([^/?#]+)$/.exec(response.headers.get('Location') ?? '')?.[1];
  if (!tag) throw new Error(`No release tag in the ${response.status} response from GitHub`);

  latest = { tag, expires: Date.now() + LATEST_TTL_MS };
  return tag;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const match = /^\/download\/gui\/([^/]+)\/?$/.exec(new URL(request.url).pathname);
    if (!match) return env.ASSETS.fetch(request);

    if (request.method !== 'GET' && request.method !== 'HEAD') {
      return new Response('Method not allowed', { status: 405, headers: { Allow: 'GET, HEAD' } });
    }

    const fileName = files[match[1]];
    if (!fileName) return new Response('Not found', { status: 404 });

    let tag: string;
    try {
      tag = await latestTag();
    } catch (error) {
      console.error('Could not resolve the latest release', error);
      return new Response('The latest release could not be found. Try again shortly.', { status: 502 });
    }

    // The redirect GitHub returns for an asset points at a signed URL that expires within minutes, so the
    // Worker follows it per request instead of caching it.
    const asset = `https://github.com/${REPO}/releases/download/${tag}/${fileName(tag.replace(/^v/, ''))}`;
    let upstream: Response;
    try {
      upstream = await fetch(asset);
    } catch (error) {
      console.error('Could not fetch the release asset', asset, error);
      return new Response('The installer could not be fetched from GitHub. Try again shortly.', { status: 502 });
    }
    if (!upstream.ok) {
      console.error('GitHub returned an error for the release asset', asset, upstream.status);
      return new Response('Not found', { status: upstream.status === 404 ? 404 : 502 });
    }

    const headers = new Headers({ 'Cache-Control': 'public, max-age=300' });
    for (const name of ['Content-Type', 'Content-Length', 'Content-Disposition']) {
      const value = upstream.headers.get(name);
      if (value) headers.set(name, value);
    }
    return new Response(upstream.body, { status: 200, headers });
  },
};

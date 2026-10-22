# Console screenshots

Generates the desktop console screenshots shown on the website (`website/public/screenshots/`). No cluster is needed: `mock.js` replaces the Wails Go bindings with a believable project ("vision-lab" on cluster "gpu-prod"), and `shoot.mjs` drives Chrome through the Vite dev server of `gui/frontend`.

Each view is captured in English and Chinese, light and dark, and written as `<view>-<lang>-<theme>.webp` (2160x1500).

Requirements: Node, Google Chrome (set `CHROME` if it is not at the macOS default path) and `cwebp` (`brew install webp`).

```sh
# 1. Serve the console frontend
cd gui/frontend && npm ci && npx vite --port 5199 &

# 2. Install playwright-core somewhere outside the repo, then shoot
mkdir -p /tmp/forgeai-shots && cd /tmp/forgeai-shots && npm init -y && npm i playwright-core
cp -r <repo>/hack/gui-screenshots/* . && node shoot.mjs <repo>/website/public/screenshots
```

Set `GUI_URL` if Vite listens elsewhere. To change what a screenshot shows, edit the fixtures in `mock.js`; to add a view, add its sidebar position to `views` in `shoot.mjs` and a matching entry in `website/src/i18n/messages.ts`.

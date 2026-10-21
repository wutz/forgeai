---
version: alpha
name: forgeai (Geist + Forge Indigo)
description: The forgeai desktop console uses Vercel's Geist system as an app surface with Forge Indigo (#4f46e5, on cool zinc neutrals) as the single theme color; see the "forgeai Theme" section. The rest of this file is an analysis of Vercel's design language — the Geist system, a stark black-on-near-white developer-platform aesthetic where a single ink tone carries every heading, CTA, and border, and the only color the page allows itself is a multi-stop mesh gradient (cyan / blue / violet / magenta / amber) confined to the hero. Geist Sans drives tightly-tracked display type, Geist Mono labels the technical eyebrows, and pill-shaped black buttons mark the marketing CTAs while 6px square buttons handle app and nav chrome.

colors:
  primary: "#171717"
  on-primary: "#ffffff"
  ink: "#171717"
  body: "#4d4d4d"
  mute: "#8f8f8f"
  faint: "#a1a1a1"
  hairline: "#ebebeb"
  hairline-soft: "#f2f2f2"
  canvas: "#fafafa"
  canvas-elevated: "#ffffff"
  link: "#0070f3"
  link-deep: "#0761d1"
  link-soft: "#d3e5ff"
  error: "#ee0000"
  error-deep: "#c50000"
  warning: "#f5a623"
  warning-soft: "#ffefcf"
  warning-deep: "#ab570a"
  violet: "#7928ca"
  violet-soft: "#d8ccf1"
  cyan: "#50e3c2"
  cyan-soft: "#aaffec"
  pink: "#ff0080"
  magenta: "#eb367f"
  gradient-develop-start: "#007cf0"
  gradient-develop-end: "#00dfd8"
  gradient-preview-start: "#7928ca"
  gradient-preview-end: "#ff0080"
  gradient-ship-start: "#ff4d4d"
  gradient-ship-end: "#f9cb28"
  # forgeai theme: "Forge Indigo" (see "forgeai Theme" section)
  accent: "#4f46e5"
  accent-deep: "#4338ca"
  accent-soft: "#eef2ff"
  accent-ring: "rgba(79, 70, 229, 0.18)"
  accent-line: "rgba(79, 70, 229, 0.28)"
  accent-tint: "#e0e7ff"
  accent-tint-hover: "#d4dcfe"
  accent-tint-press: "#c7d2fe"
  app-danger: "#dc2626"
  app-danger-deep: "#991b1b"
  app-danger-soft: "#fee2e2"
  app-danger-wash: "#fef2f2"
  app-danger-press: "#fecaca"
  app-danger-line: "rgba(220, 38, 38, 0.28)"
  accent-gradient-start: "#6366f1"
  accent-gradient-end: "#7c3aed"
  success: "#059669"
  success-soft: "#d1fae5"
  # app neutrals (zinc), overriding the Geist greys in the console
  app-ink: "#18181b"
  app-body: "#52525b"
  app-mute: "#71717a"
  app-hairline: "#e4e4e7"
  app-hairline-soft: "#f4f4f5"

typography:
  display-xl:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 48px
    fontWeight: 600
    lineHeight: 48px
    letterSpacing: -2.4px
  heading-lg:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 32px
    fontWeight: 600
    lineHeight: 40px
    letterSpacing: -1.28px
  heading-md:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 20px
    fontWeight: 600
    lineHeight: 28px
    letterSpacing: -0.4px
  label-sm:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 14px
    fontWeight: 500
    lineHeight: 20px
    letterSpacing: -0.28px
  mono-eyebrow:
    fontFamily: Geist Mono, ui-monospace, SFMono-Regular, Menlo, monospace
    fontSize: 12px
    fontWeight: 500
    lineHeight: 16px
    letterSpacing: 0
  body-lg:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 16px
    fontWeight: 400
    lineHeight: 24px
    letterSpacing: 0
  body-md:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 14px
    fontWeight: 400
    lineHeight: 20px
    letterSpacing: 0
  body-sm:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 12px
    fontWeight: 400
    lineHeight: 16px
    letterSpacing: 0
  button-lg:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 16px
    fontWeight: 500
    lineHeight: 20px
    letterSpacing: 0
  button-md:
    fontFamily: Geist, Arial, sans-serif
    fontSize: 14px
    fontWeight: 500
    lineHeight: 20px
    letterSpacing: 0
  code:
    fontFamily: Geist Mono, ui-monospace, SFMono-Regular, Menlo, monospace
    fontSize: 14px
    fontWeight: 400
    lineHeight: 20px
    letterSpacing: 0

rounded:
  none: 0px
  sm: 6px
  md: 12px
  lg: 16px
  pill-category: 64px
  pill: 100px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  2xl: 40px
  3xl: 64px
  4xl: 96px
  section: 128px

components:
  nav-bar:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.body}"
    typography: "{typography.body-md}"
    borderColor: "{colors.hairline}"
    padding: "{spacing.sm} {spacing.lg}"
  nav-link:
    textColor: "{colors.body}"
    typography: "{typography.body-md}"
    rounded: "{rounded.full}"
    padding: "{spacing.xs} {spacing.sm}"
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button-lg}"
    rounded: "{rounded.pill}"
    padding: "0px 14px"
  button-secondary:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    typography: "{typography.button-lg}"
    rounded: "{rounded.pill}"
    padding: "0px 14px"
  button-primary-sm:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  button-ghost-sm:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  # forgeai app buttons: one system with four roles (see "Buttons" under forgeai Theme)
  button-default-sm:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  button-default-sm-hover:
    backgroundColor: "{colors.hairline-soft}"
  button-create-sm:
    backgroundColor: "{colors.accent-tint}"
    textColor: "{colors.accent-deep}"
    borderColor: "{colors.accent-line}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  button-create-sm-hover:
    backgroundColor: "{colors.accent-tint-hover}"
  button-create-sm-press:
    backgroundColor: "{colors.accent-tint-press}"
  button-accent-sm:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.accent-deep}"
    borderColor: "{colors.accent-line}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  button-accent-sm-hover:
    backgroundColor: "{colors.accent-soft}"
  button-accent-sm-press:
    backgroundColor: "{colors.accent-tint}"
  button-danger-sm:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.app-danger}"
    borderColor: "{colors.hairline}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  button-danger-sm-hover:
    backgroundColor: "{colors.app-danger-wash}"
    textColor: "{colors.app-danger-deep}"
    borderColor: "{colors.app-danger-line}"
  button-danger-tint-sm:
    backgroundColor: "{colors.app-danger-wash}"
    textColor: "{colors.app-danger-deep}"
    borderColor: "{colors.app-danger-line}"
    typography: "{typography.button-md}"
    rounded: "{rounded.sm}"
    padding: "0px 6px"
  button-danger-tint-sm-hover:
    backgroundColor: "{colors.app-danger-soft}"
  button-danger-tint-sm-press:
    backgroundColor: "{colors.app-danger-press}"
  button-disabled:
    backgroundColor: "{colors.hairline-soft}"
    textColor: "{colors.faint}"
    borderColor: "{colors.hairline-soft}"
  nav-item-active:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.app-ink}"
    rounded: "{rounded.sm}"
  table-row-selected:
    backgroundColor: "{colors.accent-soft}"
    borderColor: "{colors.accent}"
  input-focus:
    borderColor: "{colors.accent}"
  button-category-pill:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    typography: "{typography.button-md}"
    rounded: "{rounded.pill-category}"
    padding: "0px 16px"
  button-icon-circular:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-lg}"
    rounded: "{rounded.full}"
    padding: "0px"
  text-input:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-md}"
    rounded: "{rounded.sm}"
    padding: "{spacing.xs} {spacing.sm}"
  feature-card:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
  feature-card-elevated:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: "{spacing.lg}"
  pricing-card:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.body-md}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xl}"
  code-block:
    backgroundColor: "{colors.canvas-elevated}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    typography: "{typography.code}"
    rounded: "{rounded.md}"
    padding: "{spacing.md}"
  logo-strip:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.mute}"
    typography: "{typography.body-md}"
    padding: "{spacing.xl} {spacing.lg}"
  hero-band:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.display-xl}"
    padding: "{spacing.section} {spacing.lg}"
  cta-band:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.display-xl}"
    padding: "{spacing.4xl} {spacing.lg}"
  footer:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.body}"
    typography: "{typography.body-md}"
    borderColor: "{colors.hairline}"
    padding: "{spacing.3xl} {spacing.lg}"

---


## forgeai Theme

forgeai is a desktop console for AI training, RL and inference jobs on Kubernetes. It follows the Geist system below as an **app surface** (not marketing): 6px square buttons (`{components.button-accent-sm}` / `{components.button-ghost-sm}`), hairline cards and tables, no hero mesh gradient.

- **Theme color — Forge Indigo** (`{colors.accent}` #4f46e5): a cool, electric indigo in the register of current AI and developer tools, chosen over the earlier Forge Ember orange so the console reads calm and technical next to dense cluster data. It is used for the primary create and submit action, the active nav icon, tab underlines, meters, focus rings and input focus (`{components.input-focus}`, with a translucent `{colors.accent-ring}` 3px ring) and the logo tile. `{colors.link}` is the accent too. Accent text uses `{colors.accent-deep}` (#4338ca). The `{colors.accent-soft}` (#eef2ff) wash also marks selected rows, which carry a 2px accent bar on their left edge (`{components.table-row-selected}`). The indigo→violet gradient (`{colors.accent-gradient-start}` → `{colors.accent-gradient-end}`) is kept for the logo tile only.
- **Buttons** — one system with four roles, all 6px square (`{components.button-default-sm}`, `{components.button-create-sm}`, `{components.button-accent-sm}`, `{components.button-danger-sm}`, `{components.button-danger-tint-sm}`). Fills stay light; ink and hairline carry the page.
  - *Default*: white, hairline edge, ink label. Every neutral action (Logs, Edit, Cancel, Save as template). Hover `{colors.hairline-soft}`.
  - *Primary*: the view's create or submit action (Create sandbox, Create job, Save, dialog submit). Indigo tint `{colors.accent-tint}`, `{colors.accent-deep}` label (6.4:1 at rest, 5.8:1 on hover, 5.3:1 pressed), `{colors.accent-line}` edge. Hover `{colors.accent-tint-hover}`, press `{colors.accent-tint-press}`. A solid indigo fill was tried and read as too heavy on every page.
  - *Accent*: Start and Resume, the state-changing action on a detail view. White with an indigo edge and `{colors.accent-deep}` label (7.9:1). Hover `{colors.accent-soft}`, press `{colors.accent-tint}`. It is one step quieter than Primary, so each view has one strong action.
  - *Danger*: destructive actions inside a row or view (Delete, Purge now). White with a `{colors.app-danger}` label (4.8:1), hover `{colors.app-danger-wash}` with a `{colors.app-danger-deep}` label (7.6:1) and a `{colors.app-danger-line}` edge.
  - *Danger tint*: the confirm button of a destructive dialog only. `{colors.app-danger-wash}` at rest, `{colors.app-danger-soft}` on hover, `{colors.app-danger-press}` pressed, `{colors.app-danger-deep}` label (7.6:1 at rest, 6.8:1 on hover, 5.7:1 pressed).
  - *Ghost*: no edge, body label, for toolbar and card links.
  - *Disabled*: one grey for every role (`{components.button-disabled}`), not reduced opacity, so a pending "Starting…" reads as a grey button with an accent spinner.
  - Focus: the 2px `{colors.accent}` outline on keyboard focus, offset 2px.
- **Neutrals**: the console uses the zinc ladder (`{colors.app-ink}` #18181b, `{colors.app-body}` #52525b, `{colors.app-mute}` #71717a, `{colors.app-hairline}` #e4e4e7) in place of Geist's neutral greys. The faint blue cast sits with indigo, and mute at #71717a is 4.8:1 on white (Geist's #8f8f8f is 3.3:1). Shadows and the dialog backdrop are tinted with slate (`rgba(15, 23, 42, …)`), not pure black.
- **Status colors**: Running `{colors.success}` (#16a34a); Queued / Pending / Restarting / Stopping `{colors.warning}`; Failed `{colors.error}` (text and dot); Succeeded / Stopped / Idle `{colors.mute}`. Status is always text + dot, never color alone.
- **CSS**: `gui/frontend/src/style.css` mirrors these tokens as custom properties (`--accent`, `--accent-deep`, `--accent-soft`, `--accent-ring`, `--accent-line`, `--accent-tint`, `--accent-tint-hover`, `--accent-tint-press`, `--error-deep`, `--error-wash`, `--error-soft`, `--error-press`, `--error-line` (the `app-danger*` keys above), `--accent-gradient`, `--success`, ...); change both together. Each theme has its own values; see Dark theme.
- Ink (`{colors.ink}`) carries headings, body text and the default buttons. Colour is limited to the Primary, Accent and Danger roles above; no other accent fills.
- **Logo**: a white anvil with a four-point spark on a rounded tile (radius ≈ 22% of the side) filled with the 135° indigo→violet accent gradient. `build/appicon.png` is the 32-unit mark drawn at 824px with 100px of transparent padding on a 1024 canvas; `icon.ico` holds 16–256px PNGs of the same render. `gui/frontend/src/Logo.tsx` holds the SVG paths, reused by `frontend/public/favicon.svg`, `build/appicon.png` and `build/windows/icon.ico`; change all of them together. The mark is not rendered inside the app any more (the connect card that showed it at 40px is gone), so `Logo.tsx` is kept as the brand source only.
- **Window chrome**: the macOS title bar is hidden (inset traffic lights); the app header is the drag region. The header and sidebar share the canvas color, so the window reads as one continuous grey frame; cards are the only white sheets. The sidebar keeps a hairline on its right.

## Dark theme

The console follows the system appearance by default. `gui/frontend/src/theme.ts` sets `data-theme="dark"` on `<html>` when the OS is dark (and updates it live when the appearance changes), and `index.html` sets it before the first paint so a dark system never flashes light. The top bar has a Theme switch (monitor / sun / moon: follow system, light, dark); a choice other than system is kept in `localStorage` under `forgeai.theme`. Every colour token has a dark value in a `:root[data-theme='dark']` block, in `style.css` and `redesign.css`. Nothing else changes: the same layout, the same roles, and the same Forge Indigo accent, lifted one tone so it reads on dark surfaces.

- **Surfaces**: the page is darker than the cards, and the sidebar sits below the page, as in light. Cards are the lightest sheets, so they still read as sheets. Shadows go black, since the slate tint would vanish on dark.
- **Accent**: fills and focus rings use `indigo-400` (#818cf8, 6.3:1 on the canvas); accent text uses `indigo-300` (#a5b4fc). The primary tint is `indigo-500` at 22% alpha, so the label stays 7:1 on it. The logo tile keeps its own gradient in both themes.
- **Status**: success `emerald-400` (#34d399), warning `amber-400` (#fbbf24) with `amber-300` text, error `red-400` (#f87171) with `red-300` text on washes. Status is still text + dot.
- **Terminal and logs**: the terminal and log panes stay `#0f1117` in both themes, since they are already dark.

| Token | Light | Dark | Role |
|---|---|---|---|
| `--canvas` | #f6f7f9 | #111113 | page |
| `--chrome` | #eef0f4 | #0c0c0e | sidebar |
| `--chrome-line` | #e2e5eb | #1f1f23 | sidebar divider |
| `--surface` | #ffffff | #18181b | cards, inputs, dialogs, header row |
| `--wash` | #fafafa | #1f1f23 | row hover, dialog footer |
| `--hairline` | #e4e4e7 | #27272a | borders |
| `--hairline-soft` | #f4f4f5 | #222226 | hover fills, meter track |
| `--ink` | #18181b | #fafafa | headings, values (16.9:1 on surface) |
| `--body` | #52525b | #d4d4d8 | body text (12.0:1) |
| `--mute` | #71717a | #a1a1aa | secondary text (6.9:1) |
| `--faint` | #a1a1aa | #71717a | nav labels (4.0:1 on chrome; decorative caps) |
| `--accent` | #4f46e5 | #818cf8 | focus, icons, meters (6.3:1 on canvas) |
| `--accent-deep` | #4338ca | #a5b4fc | accent text (8.9:1 on surface) |
| `--accent-tint` | #e0e7ff | indigo-500 @ 22% | primary button, selected row |
| `--success` | #059669 | #34d399 | running (9.2:1 on surface) |
| `--warning-deep` | #b45309 | #fcd34d | warning text (9.1:1 on its wash) |
| `--error` | #dc2626 | #f87171 | failed, danger (6.4:1 on surface) |
| `--error-deep` | #991b1b | #fca5a5 | danger text on wash |
| `--scrim` | rgba(15,23,42,.32) | rgba(0,0,0,.6) | dialog backdrop |
| `--shadow-pop` | slate, low alpha | black, low alpha | dialogs |

## Console layout

The app surface follows the Geist ladder but reads as a dense console: a grouped sidebar, a page frame, and card grids of live cluster data. All of it lives in `gui/frontend/src/redesign.css`, layered after `style.css` (same tokens, new layout); `ui.tsx` holds the shared primitives so every view is assembled from the same parts.

- **App canvas** — `--canvas` #f6f7f9 (a step darker than the marketing #fafafa) so white cards read as sheets on the desktop window. Cards stay `{colors.canvas-elevated}` at `--radius-lg` (14px) with a 1px `{colors.hairline}` and a whisper shadow.
- **Window chrome** — `--chrome` #eef0f4 paints the sidebar, one step below the canvas, so navigation reads as its own surface next to the content area. The header spans the full window and is split to match: a gradient fills `0 → sidebar width` with chrome, the next 1px with `--chrome-line` (continuing the sidebar divider) and the rest with the canvas, so the top bar reads as chrome above the sidebar and as content above the page. With the sidebar hidden the header is all canvas and the toggle moves to the left edge. On this darker ground hover states step down (`rgba(15,23,42,0.05)`) rather than lifting in white.
- **Sidebar** — 224px, chrome-colored, a hairline on its right. Items are grouped under uppercase Geist Mono labels (`Workspace`, `Storage`, `Cluster`, `Admin`) at 10.5px / `{colors.faint}`. An item is a 34px row: 16px stroke icon in `{colors.mute}` + 13.5px label; the active row is a white chip with a whisper shadow, an ink label and an `{colors.accent}` icon. The cluster and project pickers sit at the top of the sidebar, above the groups: one compact row each, a 58px uppercase mono `nav-label` beside a 28px select, and picking one connects straight away.
- **Sidebar toggle** — a panel icon at the right end of the sidebar column (clear of the macOS traffic lights, which own the top-left 84px) hides the sidebar completely: the grid's first track collapses to 0, the sidebar is `display: none`, the header becomes one flat canvas surface and the toggle moves to the left edge after the traffic lights to bring it back. The state persists in `localStorage` (`forgeai.sidebar`). At ≤820px the window starts with the sidebar hidden and the toggle is the only way to open it.
- **Preferences** — at the right end of the top bar sit two segmented switches: Theme (monitor / sun / moon: follow system, light, dark, saved under `forgeai.theme`) and Language (Auto / EN / 中文, saved under `forgeai.locale`). Both stay visible when the sidebar is collapsed. A fixed language re-renders the open views at once; Auto follows the system.
- **Page frame** — `.main` pads 20/28/40px and centers a 1180px column. The view title lives in the top bar, not in the content: `PageHeader` portals into the header slot, so the bar reads `[nav toggle] [eyebrow · title · live meta] … [view actions]`. In the bar the eyebrow is 10.5px mono, the title 15px/600 and actions are 28px `btn-sm`. There is no connect screen: the app opens on the last cluster and project, and a view with no connection shows an empty state. The project and namespace are shown only in the sidebar. Below 1100px the eyebrow and connection label drop out to keep the title and actions legible; a view rendered outside the shell falls back to a title band above the content.
- **Metric tiles** — a responsive grid (`repeat(auto-fit, minmax(190px, 1fr))`). Each tile carries a 22px toned icon chip, a 12px mute label, a 26px/600 tabular value with a mute unit, a 12px detail line and an optional `Meter`. Tones: accent (in use), success (running), warning (queued), error (failed), violet (#7c3aed, a secondary tone that pairs with the indigo gradient), neutral.
- **Meters** — 6px full-radius tracks on `{colors.hairline-soft}` with a label row above (11.5px mute, tabular figures). Fill tones mirror the tiles; `muted` marks informational usage. Meters are always derived from real list data — there is no fabricated trend chart.
- **Segmented filter** — an inset `{colors.hairline-soft}` pill container holding 26px segments; the active segment is a white chip with a 1px shadow and an accent-tinted count. Used for job state filters.
- **Tabs** — underline tabs inside detail cards (34px, 2px accent underline on the active tab), for `Overview` / `Workers` / `Log` / `Env`.
- **Badges** — 20px full-radius pills at 11px/500: neutral by default, toned for status, instance type, isolation class and quota facts.
- **Tables** — hairline header row in mono uppercase 11px; 11px/16px cell padding; the first cell is a `.cell-main` / `.cell-sub` pair (name + image or template) so the list reads without extra columns. Selected rows take the accent wash and a 2px accent bar on the left.
- **Detail split** — list + 420px detail card (`minmax(0, 1fr) 420px`), collapsing to one column at ≤1100px. The detail card has a title head, an action bar (`detail-actions`) and the tab body.
- **Dialogs** — 620px (440px for small forms), `{rounded.lg}`, Level-2 floating shadow, a dimmed blurred backdrop, a title/subtitle head, a scrolling body and a footer with the cancel and primary actions.

## Overview

Vercel's Geist system is an exercise in subtraction. The page is a near-white sheet (`{colors.canvas}` — #fafafa) carrying near-black ink (`{colors.ink}` — #171717), and almost nothing else competes. Headings, body copy, primary buttons, and the thin 1px borders that define every card all draw from the same ink-and-grey ladder. The one place color is allowed to exist is the hero, where a soft multi-stop **mesh gradient** — cyan, blue, violet, magenta, amber — blooms behind or beside the headline as the brand's entire decorative system. Everywhere else, restraint.

Typography does the heavy lifting. **Geist Sans** sets the display headline in tightly-tracked weight-600 (the hero h1 runs -2.4px letter-spacing), and **Geist Mono** appears as small uppercase eyebrows labeling sections like a technical spec sheet. Buttons split into two shapes by context: the marketing CTAs are fully rounded black **pills** (`{rounded.pill}` — 100px, "Start Deploying" / "Get a Demo"), while nav and in-app controls use a tight 6px square (`{rounded.sm}`, "Sign Up" / "Log In"). The contrast between the rounded marketing pill and the square app button is itself a deliberate signal of which surface you're on.

Surfaces barely lift. Cards are white (`{colors.canvas-elevated}`) on the #fafafa canvas, separated by a 1px hairline (`{colors.hairline}` — #ebebeb) and, at most, a whisper-soft layered shadow. Feature sections are built from precise grids of these hairline cards, often holding thin node-graph or code-editor illustrations rendered in the same ink-on-white palette. The page reads like documentation that happens to be selling something — engineered, exact, and confident enough to let a single gradient be the only flourish.

**Key Characteristics:**
- A single near-black ink (`{colors.ink}`) carries headings, body, primary CTAs, and borders on a near-white canvas (`{colors.canvas}`) — near-zero chromatic chrome.
- The multi-stop mesh gradient (cyan → blue → violet → magenta → amber) is the entire decorative system, confined to the hero.
- Two button shapes by context: rounded black **pills** (`{rounded.pill}`) for marketing CTAs, tight 6px squares (`{rounded.sm}`) for nav/app controls.
- Geist Sans for tightly-tracked display type (`{typography.display-xl}` at -2.4px), Geist Mono for uppercase technical eyebrows (`{typography.mono-eyebrow}`).
- Hairline-bordered white cards (`{colors.hairline}` on `{colors.canvas-elevated}`) in precise grids; depth via 1px border + whisper shadow, never heavy elevation.
- The classic Vercel gradient trio (develop/preview/ship) survives as a named accent system: `{colors.gradient-develop-start}`→end, preview, ship.
- Color-block page rhythm: white hero with mesh gradient → logo strip → hairline feature-card grid → code-editor band → template cards → black-text CTA band → grey footer.

## Colors

> Source pages analyzed: the home page, the AI Gateway page, the customers page, and the pricing page. The ink/canvas/hairline trio recurs on every page; the accent blue (`{colors.link}`) surfaces on pricing, and the mesh-gradient stops live in the hero.

### Brand & Accent
- **Ink** (`{colors.primary}` / `{colors.ink}` — #171717): the brand's defining near-black. Headings, primary CTA fill, logo, and the darkest text tier. Paired with `{colors.on-primary}` (white).
- **Vercel Blue** (`{colors.link}` — #0070f3): the link and accent blue — inline links, pricing highlights, focus signals. Darker press tone `{colors.link-deep}` (#0761d1), pale wash `{colors.link-soft}` (#d3e5ff).
- **Violet** (`{colors.violet}` — #7928ca), **Cyan** (`{colors.cyan}` — #50e3c2), **Pink** (`{colors.pink}` — #ff0080), **Magenta** (`{colors.magenta}` — #eb367f): the chromatic accent family, used sparingly for illustration accents and as mesh-gradient stops, never as chrome fills.

### Surface
- **Canvas** (`{colors.canvas}` — #fafafa): the default page background — the near-white sheet everything sits on.
- **Elevated** (`{colors.canvas-elevated}` — #ffffff): pure white for cards, buttons, inputs, and code blocks lifted off the canvas.
- **Hairline-Soft Surface** (`{colors.hairline-soft}` — #f2f2f2): the faintest grey fill for subtle alternating panels and inset wells.

### Text
- **Ink** (`{colors.ink}` — #171717): primary headings and high-emphasis text.
- **Body** (`{colors.body}` — #4d4d4d): standard paragraph and secondary copy, nav links.
- **Mute** (`{colors.mute}` — #8f8f8f): lower-emphasis captions, logo-strip labels, metadata.
- **Faint** (`{colors.faint}` — #a1a1a1): the lowest tier — placeholders, disabled labels.

### Borders
- **Hairline** (`{colors.hairline}` — #ebebeb): the 1px border on every card, input, and divider — the structural workhorse of the system.

### Semantic
- **Error** (`{colors.error}` — #ee0000): validation / destructive, with a deep press tier `{colors.error-deep}` (#c50000).
- **Warning** (`{colors.warning}` — #f5a623): caution states, with soft `{colors.warning-soft}` and deep `{colors.warning-deep}` tiers.
- **Success** maps to `{colors.link}` (#0070f3) on Vercel — the blue doubles as the positive/active signal. forgeai uses a distinct green `{colors.success}` (#16a34a, soft `{colors.success-soft}`) instead, so running jobs are not confused with links.

### Brand Gradient
Three named two-stop gradients form the legacy Vercel gradient identity, surviving as illustration and accent washes:
- **Develop**: `{colors.gradient-develop-start}` (#007cf0) → `{colors.gradient-develop-end}` (#00dfd8) — blue to cyan.
- **Preview**: `{colors.gradient-preview-start}` (#7928ca) → `{colors.gradient-preview-end}` (#ff0080) — violet to pink.
- **Ship**: `{colors.gradient-ship-start}` (#ff4d4d) → `{colors.gradient-ship-end}` (#f9cb28) — red to amber.
These, blended together, form the hero's multi-stop mesh.

## Typography

### Font Family
The system runs entirely on **Geist** — Vercel's own type family. **Geist Sans** (with an `Arial` system fallback) sets all UI and prose; **Geist Mono** sets code, inline technical tokens, and the small uppercase section eyebrows. There is no third face. Geist Sans is a clean geometric-humanist sans; substitute **Inter** if Geist is unavailable, and **JetBrains Mono** or **IBM Plex Mono** for Geist Mono.

### Hierarchy

| Token | Size | Weight | Line Height | Letter Spacing | Use |
|---|---|---|---|---|---|
| `{typography.display-xl}` | 48px | 600 | 48px | -2.4px | Hero headline |
| `{typography.heading-lg}` | 32px | 600 | 40px | -1.28px | Major section headings |
| `{typography.heading-md}` | 20px | 600 | 28px | -0.4px | Sub-section / card headings |
| `{typography.label-sm}` | 14px | 500 | 20px | -0.28px | Strong labels, nav emphasis |
| `{typography.mono-eyebrow}` | 12px | 500 | 16px | 0 | Uppercase Geist Mono section eyebrows |
| `{typography.body-lg}` | 16px | 400 | 24px | 0 | Lead paragraphs, large body |
| `{typography.body-md}` | 14px | 400 | 20px | 0 | Default body, nav links, table cells |
| `{typography.body-sm}` | 12px | 400 | 16px | 0 | Captions, footnotes, metadata |
| `{typography.button-lg}` | 16px | 500 | 20px | 0 | Marketing pill button labels |
| `{typography.button-md}` | 14px | 500 | 20px | 0 | Nav / app button labels |
| `{typography.code}` | 14px | 400 | 20px | 0 | Code blocks, inline code (Geist Mono) |

### Principles
- Display type is defined by tight negative tracking — the larger the heading, the tighter (-2.4px at hero scale, -1.28px at section scale). Body type sits at neutral spacing.
- Weight is binary: 600 for headings and 500 for buttons/labels; everything else is 400. There is no light or black weight, and no italic.
- Geist Mono is reserved for two roles only — code, and the small uppercase eyebrow labels that introduce sections like spec-sheet headers.

### Note on Font Substitutes
Geist Sans and Geist Mono are freely available (open-source, via Vercel / Google Fonts). If unavailable, **Inter** (sans) and **JetBrains Mono** (mono) are the closest open substitutes; keep heading weight at 600 and preserve the negative display tracking.

## Layout

### Spacing System
- **Base unit**: 4px. The scale steps 4 → 8 → 12 → 16 → 24 → 32 → 40 → 64 → 96 → 128px.
- **Tokens**: `{spacing.xxs}` 4px · `{spacing.xs}` 8px · `{spacing.sm}` 12px · `{spacing.md}` 16px · `{spacing.lg}` 24px · `{spacing.xl}` 32px · `{spacing.2xl}` 40px · `{spacing.3xl}` 64px · `{spacing.4xl}` 96px · `{spacing.section}` 128px.
- **Card interiors** sit at `{spacing.lg}`–`{spacing.xl}` (24–32px); **section bands** run `{spacing.4xl}`–`{spacing.section}` (96–128px) of vertical rhythm.
- **Button padding** is horizontal-only — marketing pills run `0px 14px`, nav buttons `0px 6px` — with height set by line-height rather than vertical padding.

### Grid & Container
- Centered max-width container (~1200px) with comfortable gutters; the hero and CTA bands center their content.
- Feature sections use 2-up, 3-up, and 4-up hairline-card grids that collapse toward 1-up on narrow widths.
- The pricing page uses a multi-column tier grid; the customers page a logo / case-study grid.

### Whitespace Philosophy
Whitespace is structural. The near-white canvas and generous section padding do the separating work; cards are grouped by thin hairlines rather than heavy backgrounds. The page breathes — large vertical gaps between bands, tight internal rhythm inside cards.

### Responsive Strategy

#### Breakpoints
| Name | Width | Key Changes |
|---|---|---|
| Mobile | ≤ 640px | Single-column stacks; nav → menu trigger; hero type scales down; pill CTAs go full-width |
| Tablet | 768px | 2-up card grids; condensed nav |
| Laptop | 1024px | 3–4-up grids; full nav row |
| Desktop | 1200px+ | Centered max-width container, full multi-column grids |

#### Touch Targets
Marketing pill CTAs (`{components.button-primary}`) and nav buttons clear the 44px WCAG-AAA target via line-height-driven height. Circular icon buttons (`{components.button-icon-circular}`) keep adequate hit area.

#### Collapsing Strategy
The nav row collapses behind a menu trigger; multi-column hairline-card grids reflow to a single column; code-editor and node-graph illustrations scale or scroll rather than shrink illegibly; the pricing tier grid stacks vertically.

#### Image Behavior
The hero mesh gradient is a CSS/SVG composition that scales fluidly. Feature illustrations (node graphs, code editors) are vector/HTML, ink-on-white, scaling crisply. Customer logos sit in a greyscale strip. No heavy raster photography.

## Elevation & Depth

| Level | Treatment | Use |
|---|---|---|
| 0 — Flat | 1px hairline (`{colors.hairline}`), no shadow | Default feature cards, inputs, dividers, the canvas |
| 1 — Whisper | Border + `0px 1px 1px rgba(0,0,0,0.04)` micro-shadow | Lightly-raised cards |
| 2 — Floating | Layered soft shadow (`0px 2px 2px` + `0px 8px 16px -4px` low-alpha black) + inset hairline | Menus, modals, tooltips |

Depth is deliberately minimal. The system prefers a crisp 1px hairline plus the near-white-on-white surface step to a shadow; when a surface floats, it uses a finely-layered, very-low-alpha shadow stack rather than a single heavy drop.

### Decorative Depth
The hero **mesh gradient** is the only atmospheric element — a soft multi-stop bloom of the brand accent colors against the white canvas. Feature illustrations (ink node-graphs, code editors) add a sense of product depth without color. No glows, no heavy gradients elsewhere.

## Shapes

### Border Radius Scale

| Token | Value | Use |
|---|---|---|
| `{rounded.none}` | 0px | Full-bleed bands, dividers |
| `{rounded.sm}` | 6px | Nav / app buttons, inputs |
| `{rounded.md}` | 12px | Feature cards, code blocks |
| `{rounded.lg}` | 16px | Pricing cards, larger panels |
| `{rounded.pill-category}` | 64px | Category-tab pills (AI Apps / Web Apps) |
| `{rounded.pill}` | 100px | Marketing CTA pills |
| `{rounded.full}` | 9999px | Circular icon buttons, avatars, nav ghost links |

The radius language is bimodal: tight 6px squares for functional chrome, full pills for marketing CTAs and category tabs, with 12–16px on content cards in between.

### Geometry
Cards are rectangles at 12–16px radius; marketing buttons and category tabs are full pills; icon buttons and avatars are circular. Illustrations are line-weight vector graphics in ink on white.

## Components

> No hover states are documented. Each spec covers Default and (where extracted) pressed/active states. Variants live as separate `components:` entries.

### Navigation

**`nav-bar`** — top navigation
- Background `{colors.canvas}`, bottom hairline `{colors.hairline}`, text `{colors.body}`, type `{typography.body-md}`, padding `{spacing.sm} {spacing.lg}`. Holds the black wordmark, ghost nav links, and the Sign Up / Log In buttons at right.

**`nav-link`** — individual nav item
- Body-grey text `{colors.body}`, type `{typography.body-md}`, fully rounded hit area `{rounded.full}`, padding `{spacing.xs} {spacing.sm}`. Transparent until interacted.

### Buttons

**`button-primary`** — the black marketing pill ("Start Deploying", "Deploy")
- Background `{colors.primary}`, text `{colors.on-primary}`, type `{typography.button-lg}`, fully rounded `{rounded.pill}` (100px), padding `0px 14px`.

**`button-secondary`** — the white marketing pill ("Get a Demo")
- Background `{colors.canvas-elevated}`, text `{colors.ink}`, type `{typography.button-lg}`, rounded `{rounded.pill}`, padding `0px 14px`. Same pill shape as primary, inverted fill.

**`button-primary-sm`** — the compact black nav CTA ("Sign Up")
- Background `{colors.primary}`, text `{colors.on-primary}`, type `{typography.button-md}`, tight square `{rounded.sm}` (6px), padding `0px 6px`.

**`button-ghost-sm`** — the white nav/app button ("Log In", "Ask AI")
- Background `{colors.canvas-elevated}`, text `{colors.ink}`, 1px hairline `{colors.hairline}`, type `{typography.button-md}`, rounded `{rounded.sm}`, padding `0px 6px`.

**`button-category-pill`** — the category-tab pill ("AI Apps", "Web Apps", "Ecommerce")
- Background `{colors.canvas-elevated}`, text `{colors.ink}`, type `{typography.button-md}`, rounded `{rounded.pill-category}` (64px), padding `0px 16px`.

**`button-icon-circular`** — circular icon / carousel control
- Background `{colors.canvas-elevated}`, text `{colors.ink}`, 1px hairline `{colors.hairline}`, type `{typography.body-lg}`, rounded `{rounded.full}`, no padding.

### Inputs & Forms

**`text-input`** — default form field
- Background `{colors.canvas-elevated}`, ink text `{colors.ink}`, 1px hairline `{colors.hairline}`, type `{typography.body-md}`, rounded `{rounded.sm}`, padding `{spacing.xs} {spacing.sm}`.

### Cards & Containers

**`feature-card`** — flat hairline content card
- Background `{colors.canvas-elevated}`, 1px hairline `{colors.hairline}`, ink text `{colors.ink}`, type `{typography.body-md}`, rounded `{rounded.md}`, padding `{spacing.lg}`. The workhorse grid tile, often holding a node-graph or code illustration.

**`feature-card-elevated`** — lifted card variant
- Same chrome as `feature-card` with the Level-2 floating shadow for menus / featured tiles.

**`pricing-card`** — pricing tier card
- Background `{colors.canvas-elevated}`, 1px hairline `{colors.hairline}`, ink text `{colors.ink}`, type `{typography.body-md}`, rounded `{rounded.lg}`, padding `{spacing.xl}`.

**`code-block`** — code / terminal surface
- Background `{colors.canvas-elevated}`, ink text `{colors.ink}`, 1px hairline `{colors.hairline}`, monospace `{typography.code}`, rounded `{rounded.md}`, padding `{spacing.md}`. Syntax rendered in the ink-and-accent palette.

### Bands

**`logo-strip`** — customer logo band
- Background `{colors.canvas}`, mute text `{colors.mute}`, type `{typography.body-md}`, padding `{spacing.xl} {spacing.lg}`. A greyscale row of customer wordmarks.

**`hero-band`** — full-width hero section
- Background `{colors.canvas}` with the mesh gradient, ink text `{colors.ink}`, display type `{typography.display-xl}`, padding `{spacing.section} {spacing.lg}`.

**`cta-band`** — end-of-page call-to-action band ("Start Deploying")
- Background `{colors.canvas}`, ink text `{colors.ink}`, display type `{typography.display-xl}`, padding `{spacing.4xl} {spacing.lg}`, with a `{components.button-primary}` pill.

### Footer

**`footer`** — site footer
- Background `{colors.canvas}`, top hairline `{colors.hairline}`, body-grey text `{colors.body}`, type `{typography.body-md}`, padding `{spacing.3xl} {spacing.lg}`. Multi-column link groups under the wordmark.

## Do's and Don'ts

### Do
- Keep the canvas near-white (`{colors.canvas}`) and let near-black ink (`{colors.ink}`) carry headings, CTAs, and borders — the system is a black-and-white duet.
- Confine color to the hero mesh gradient and small illustration accents; reserve `{colors.link}` for links and focus.
- Use the two button shapes by context: black pill (`{components.button-primary}`) for marketing CTAs, 6px square (`{components.button-primary-sm}`) for nav/app.
- Define cards and inputs with a 1px hairline (`{colors.hairline}`) before any shadow — flat is the default.
- Set display headings in Geist Sans 600 with tight negative tracking; label sections with uppercase Geist Mono eyebrows (`{typography.mono-eyebrow}`).
- Step the grey text ladder deliberately: `{colors.ink}` → `{colors.body}` → `{colors.mute}` → `{colors.faint}`.

### Don't
- Don't fill large surfaces with the accent colors — violet/cyan/pink/blue live in the gradient and illustrations, not as chrome.
- Don't mix the button shapes within one context — marketing CTAs stay pills, app/nav controls stay 6px squares.
- Don't pile on shadows — depth is a 1px hairline plus, at most, a finely-layered low-alpha shadow stack.
- Don't set body copy in pure black (`#000000`) — the brand's ink is #171717 and body steps to `{colors.body}`.
- Don't add a second decorative system — the mesh gradient is the only flourish; everything else is ink on white.
- Don't loosen the display tracking — large Geist headings carry tight negative letter-spacing by design.

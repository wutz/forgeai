// Logo is the forgeai mark: a white anvil with a spark on a Forge Ember tile.
// The paths are also used for gui/build/appicon.png and the favicon; keep
// them in sync.
export function Logo({size = 22}: {size?: number}) {
  return (
    <svg className="logo-mark" width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <rect width="32" height="32" rx="7" fill="var(--ember)"/>
      <g fill="#fff" transform="translate(5.1 6.1) scale(0.68)">
        <path d="M3 10 10 9h18v5h-5l-3 4v3l4 3v2H8v-2l4-3v-3l-2-3-3-1.5z"/>
        <path d="m22 1 .9 2.3 2.3.9-2.3.9L22 7.4l-.9-2.3-2.3-.9 2.3-.9z"/>
      </g>
    </svg>
  )
}

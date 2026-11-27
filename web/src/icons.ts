// Interface glyphs use a common 24px drawing grid. Button hit areas are set in CSS.
const icons = {
  back: '<path d="m11 5-7 7 7 7M4 12h16"/>',
  home: '<path d="m3 10 9-7 9 7M5 9v11h5v-6h4v6h5V9"/>',
  switcher: '<rect x="3" y="7" width="13" height="13" rx="2"/><path d="M8 4h11a2 2 0 0 1 2 2v9"/>',
  power: '<path d="M12 3v8m5.5-5.2a8 8 0 1 1-11 0"/>',
  volup: '<path d="M3 9h4l5-4v14l-5-4H3zm12 0a4 4 0 0 1 0 6m3-9a8 8 0 0 1 0 12"/>',
  voldown: '<path d="M3 9h4l5-4v14l-5-4H3zm12 0a4 4 0 0 1 0 6"/>',
  mute: '<path d="M3 9h4l5-4v14l-5-4H3zm13 0 5 6m0-6-5 6"/>',
  paste: '<rect x="8" y="3" width="8" height="4" rx="1"/><path d="M8 5H5v16h14V5h-3M8 12h8m-8 4h5"/>',
  selectall: '<path d="M8 4H4v4m12-4h4v4M4 16v4h4m12-4v4h-4M8 10h8m-8 4h6"/>',
  clear: '<path d="M20 6H9l-6 6 6 6h11a1 1 0 0 0 1-1V7a1 1 0 0 0-1-1zm-8 3 6 6m0-6-6 6"/>',
  shot: '<rect x="6" y="6" width="15" height="15" rx="2"/><path d="M3 15V5a2 2 0 0 1 2-2h10M6 17l5-5 4 4 2-2 4 4"/><circle cx="17" cy="10" r=".7"/>',
  rename: '<path d="m4 16-1 5 5-1L21 7l-4-4zM14 6l4 4"/>',
  expand: '<path d="M13 4h7v7m0-7-5 5M11 20H4v-7m0 7 5-5"/>',
  full: '<path d="M9 4H4v5m11-5h5v5M4 15v5h5m11-5v5h-5"/>',
  hash: '<path d="M4 9h16M4 15h16M10 4 8 20m8-16-2 16"/>',
  link: '<path d="M9 16H7a4 4 0 0 1 0-8h2m6 0h2a4 4 0 0 1 0 8h-2M8 12h8"/>',
  unlink: '<path d="M8 16H7a4 4 0 0 1-3-6.6M16 8h1a4 4 0 0 1 3 6.6M4 4l16 16"/>',
  all: '<rect x="4" y="4" width="16" height="16" rx="2"/><path d="m8 12 3 3 5-6"/>',
  none: '<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 12h8"/>',
  park: '<rect x="6" y="3" width="12" height="18" rx="2"/><path d="M10 9v6m4-6v6"/>',
  restore: '<rect x="6" y="3" width="12" height="18" rx="2"/><path d="m10 9 5 3-5 3z"/>',
  cursor: '<path d="m4 4 15 6-7 2-2 7z"/>',
  chev: '<path d="m6 7 5 5-5 5m7-10 5 5-5 5"/>',
  chevl: '<path d="m11 7-5 5 5 5m7-10-5 5 5 5"/>',
  moon: '<path d="M20 15a8.5 8.5 0 0 1-11-11 8.5 8.5 0 1 0 11 11Z"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 3v2m0 14v2M3 12h2m14 0h2M6 6l1.5 1.5m9 9L18 18m0-12-1.5 1.5m-9 9L6 18"/>',
  refresh: '<path d="M20 4v5h-5m5 0a8 8 0 1 0 0 6"/>',
  x: '<path d="m6 6 12 12M18 6 6 18"/>',
  trash: '<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13m-8 4v5m4-5v5"/>',
  cc: '<path d="M4 7h4m4 0h8M4 17h8m4 0h4"/><circle cx="10" cy="7" r="2"/><circle cx="14" cy="17" r="2"/>',
};

export type IconName = keyof typeof icons;

export function ico(name: IconName) {
  return `<svg class="ui-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">${icons[name]}</svg>`;
}

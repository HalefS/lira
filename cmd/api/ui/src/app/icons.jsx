/* ── icons ── */
const Ic = {
  logo:   <svg viewBox="0 0 20 20" fill="white"><rect x="2" y="2" width="7" height="7" rx="1.5"/><rect x="11" y="2" width="7" height="7" rx="1.5" opacity=".6"/><rect x="2" y="11" width="7" height="7" rx="1.5" opacity=".6"/><rect x="11" y="11" width="7" height="7" rx="1.5" opacity=".3"/></svg>,
  grid:   <svg viewBox="0 0 16 16" fill="currentColor"><rect x="1" y="1" width="6" height="6" rx="1"/><rect x="9" y="1" width="6" height="6" rx="1"/><rect x="1" y="9" width="6" height="6" rx="1"/><rect x="9" y="9" width="6" height="6" rx="1"/></svg>,
  // A floor plan rather than the four-square grid: the room map is rooms
  // divided by walls, and using the dashboard's own grid would make two
  // different destinations share a mark.
  map:    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round"><rect x="1.8" y="1.8" width="12.4" height="12.4" rx="1"/><path d="M8 1.8v5.4M1.8 7.2H8M8 9.1h6.2M12.2 7.2v7M8 9.1v5.1"/></svg>,
  // Wi-Fi: two arcs and the origin dot, legible at the 14px an equipment row
  // gives it. The lock is a shackle over a body, not a keyhole silhouette.
  wifi:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"><path d="M2.2 6.1a8.6 8.6 0 0 1 11.6 0"/><path d="M4.5 8.7a5.2 5.2 0 0 1 7 0"/><path d="M6.8 11.2a2.2 2.2 0 0 1 2.4 0"/><circle cx="8" cy="13.4" r=".5" fill="currentColor" stroke="none"/></svg>,
  lock:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><rect x="3.2" y="7" width="9.6" height="7" rx="1.4"/><path d="M5.4 7V5.1a2.6 2.6 0 0 1 5.2 0V7"/><path d="M8 10.2v.8"/></svg>,
  menu:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="M2 4h12M2 8h12M2 12h12"/></svg>,
  list:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M2 4h12M2 8h12M2 12h8"/></svg>,
  clock:  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="8" cy="8" r="6"/><path d="M8 5v3l2 2"/></svg>,
  users:  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="6" cy="5" r="2.5"/><path d="M1 13c0-2.8 2.2-5 5-5"/><circle cx="12" cy="7" r="2"/><path d="M10 13c0-2 1.3-3.5 3-4"/></svg>,
  plus:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M8 2v12M2 8h12"/></svg>,
  minus:  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M2 8h12"/></svg>,
  search: <svg viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.4"><circle cx="6" cy="6" r="4"/><path d="M10 10l2.5 2.5" strokeLinecap="round"/></svg>,
  x:      <svg viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="M2 2l10 10M12 2L2 12"/></svg>,
  trash:  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M2 4h12M5 4V3a1 1 0 011-1h4a1 1 0 011 1v1M6 7v5M10 7v5M3 4l1 9h8l1-9"/></svg>,
  edit:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M11 2l3 3-8 8H3v-3l8-8z"/></svg>,
  check:  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M3 8l3.5 3.5L13 4"/></svg>,
  logout: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M6 2H3a1 1 0 00-1 1v10a1 1 0 001 1h3M10 11l3-3-3-3M13 8H6"/></svg>,
  // Install to this device: an arrow into a screen, not into a file. `download`
  // next to it is a report leaving the app, which is a different promise.
  install: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"><rect x="2.5" y="2.5" width="11" height="11" rx="2"/><path d="M8 4.8v5M5.6 7.6L8 10l2.4-2.4"/></svg>,
  shield: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M8 2l5 2v4c0 3-2.5 5.5-5 6C5.5 13.5 3 11 3 8V4l5-2z"/></svg>,
  // The gear, for the settings destination. A hub plus eight teeth rather than a
  // full cog outline: at the 15px the nav gives it, an outlined gear closes up
  // into a blob, whereas separate strokes stay countable. The teeth start at r=2.4
  // and the hub's own stroke reaches r=2.95, so they overlap and read as one
  // shape -- drawn clear of the hub instead, the gap is about a pixel at this size
  // and the whole thing looks like a sun. Distinct from `tool` (the wrench, which
  // means maintenance) and from `shield`, which still means "manager" everywhere it
  // is genuinely about a person's role.
  gear:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"><circle cx="8" cy="8" r="2.2"/><path d="M8 3.4v2.2M8 10.6v2.2M12.6 8h-2.2M5.4 8H3.2M9.7 6.3l1.55-1.55M9.7 9.7l1.55 1.55M6.3 9.7l-1.55 1.55M6.3 6.3L4.75 4.75"/></svg>,
  tool:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M10.5 2a3.5 3.5 0 00-3.3 4.6L2 11.8 4.2 14l5.2-5.2A3.5 3.5 0 1010.5 2z"/></svg>,
  eye:    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M1 8s2.5-5 7-5 7 5 7 5-2.5 5-7 5-7-5-7-5z"/><circle cx="8" cy="8" r="2"/></svg>,
  eyeoff: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M2 2l12 12M6.5 6.6A2 2 0 0010 9.5M4 4.3C2.4 5.5 1 8 1 8s2.5 5 7 5a7 7 0 003-.7M7 3.1A7 7 0 0115 8s-.8 1.7-2 3"/></svg>,
  info:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="8" cy="8" r="6"/><path d="M8 5v3M8 11v.5"/></svg>,
  person: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="8" cy="5" r="3"/><path d="M2 14c0-3.3 2.7-6 6-6s6 2.7 6 6"/></svg>,
  upload: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M8 10V3M5 6l3-3 3 3"/><path d="M2 13h12"/></svg>,
  camera: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M1 5a1 1 0 011-1h1.5l1-2h5l1 2H13a1 1 0 011 1v7a1 1 0 01-1 1H2a1 1 0 01-1-1z"/><circle cx="8" cy="8.5" r="2.5"/></svg>,
  bell:   <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M8 2a3 3 0 00-3 3v2c0 1-.5 2-1.3 2.7A.8.8 0 004 11h8a.8.8 0 00.3-1.3C11.5 9 11 8 11 7V5a3 3 0 00-3-3z"/><path d="M6.5 13a1.5 1.5 0 003 0"/></svg>,
  // A tray, not a bell: Alerts already owns the bell, and the inbox is a
  // personal unread state rather than a second queue of recurring issues.
  inbox:  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M2.2 8.2h3.2l1 1.8h3.2l1-1.8h3.2"/><path d="M4.1 3h7.8l2 5.2v3.6a1.2 1.2 0 01-1.2 1.2H3.3a1.2 1.2 0 01-1.2-1.2V8.2z"/></svg>,
  // A set on a stand, for the TV swap strip. Kept small and legible at the
  // 11px the strips render at.
  tv:     <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4"><rect x="2" y="3.5" width="12" height="8" rx="1.6"/><path d="M8 11.5v1.8M5.6 14.2h4.8"/></svg>,
  trending: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M2 11l4-4 2.5 2.5L14 4"/><path d="M10 4h4v4"/></svg>,
  refresh: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M13.7 8a5.7 5.7 0 10-1.7 4.1"/><path d="M13.7 4.3V8h-3.7"/></svg>,
  server: <svg viewBox="0 0 16 16"  fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="2" y="2.5" width="12" height="4.2" rx="1"/><rect x="2" y="9.3" width="12" height="4.2" rx="1"/><path d="M4.3 4.6h.01M4.3 11.4h.01" strokeWidth="2"/></svg>,
  cpu: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="4.5" y="4.5" width="7" height="7" rx="1"/><rect x="6.5" y="6.5" width="3" height="3"/><path d="M8 1.2v2M8 12.8v2M1.2 8h2M12.8 8h2M4.9 1.2l.7 1.9M11.1 1.2l-.7 1.9M4.9 14.8l.7-1.9M11.1 14.8l-.7-1.9"/></svg>,
  database: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><ellipse cx="8" cy="3.3" rx="5.5" ry="2"/><path d="M2.5 3.3v9.4c0 1.1 2.5 2 5.5 2s5.5-.9 5.5-2V3.3"/><path d="M2.5 8c0 1.1 2.5 2 5.5 2s5.5-.9 5.5-2"/></svg>,
  activity: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round"><path d="M1.5 8.5h3l1.6-4.8 2.8 9.6L10.8 8.5h3.7"/></svg>,
  box: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M2 5l6-3 6 3-6 3-6-3z"/><path d="M2 5v6l6 3 6-3V5"/><path d="M8 8v6"/></svg>,
  report: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="2" y="1" width="12" height="14" rx="1"/><path d="M5 5h6M5 8h6M5 11h4"/></svg>,
  download: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"><path d="M8 2v8M5 7l3 3 3-3"/><path d="M2 13h12"/></svg>,
  calendar: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><rect x="1" y="3" width="14" height="12" rx="1"/><path d="M1 7h14M5 1v4M11 1v4"/></svg>,
  chevron: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M6 3.5l4.5 4.5L6 12.5"/></svg>,
  // Printer: paper standing out of the top tray, the body with its status light,
  // and the sheet coming out of the front. Drawn to read at 16px, which is why the
  // tray is a separate outlined path rather than filled.
  printer: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M4.6 5.7V2.5a.9.9 0 0 1 .9-.9h5a.9.9 0 0 1 .9.9v3.2"/>
    <rect x="1.7" y="5.7" width="12.6" height="4.9" rx="1.3"/>
    <path d="M4.4 9.5h7.2v4.9H4.4z"/>
    <path d="M12.3 7.7h.01" strokeWidth="2.1"/>
  </svg>,
  // Phone: the handset silhouette rather than a handset-shaped blob, so it still
  // reads as a phone at 16px and beside the printer's squared-off shape.
  phone: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M4.7 1.7 3.3 3.1a1.7 1.7 0 0 0-.3 2c1.7 3.8 4.8 6.9 8.6 8.6a1.7 1.7 0 0 0 2-.3l1.4-1.4a1.2 1.2 0 0 0 0-1.7l-1.9-1.9a1.2 1.2 0 0 0-1.7 0l-.7.7a11 11 0 0 1-4.5-4.5l.7-.7a1.2 1.2 0 0 0 0-1.7L5.6 1.7a1.2 1.2 0 0 0-1.7 0Z"/>
  </svg>,
  // Pin: the map marker rather than a building or a door, because it is used for
  // "where this is", not "what kind of place this is". The hole is left as a
  // separate circle so it reads at 12px, where a filled dot would close up.
  pin: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M13 6.5c0 3.6-5 8-5 8s-5-4.4-5-8a5 5 0 1 1 10 0Z"/>
    <circle cx="8" cy="6.4" r="1.7"/>
  </svg>,
  // A paint palette with a thumbhole and three dabs. Not a droplet: at 14px a
  // droplet reads as "liquid" and this is a colour, not a quantity.
  palette: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round"><path d="M8 1.9a6.1 6.1 0 000 12.2c.9 0 1.4-.6 1.4-1.3 0-.8-.7-1.1-.7-1.8 0-.6.5-1.1 1.2-1.1h1.2a3 3 0 003-3C14.1 4 11.4 1.9 8 1.9z"/><circle cx="5.2" cy="6.6" r=".9"/><circle cx="8" cy="4.7" r=".9"/><circle cx="10.9" cy="6.1" r=".9"/></svg>,
  // Four corner brackets: the "select a run" mark. Drawn as four corner strokes
  // rather than a closed rectangle because at the 11px a grid cell gives it, a
  // closed box reads as a border on the cell and corners read as something you can
  // pull. Used twice at two sizes -- as the drag affordance on a cell, and in the
  // save bar as the range-mode toggle.
  brackets: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
    <path d="M2.6 6.2V3.3a.7.7 0 01.7-.7h2.9"/>
    <path d="M9.8 2.6h2.9a.7.7 0 01.7.7v2.9"/>
    <path d="M13.4 9.8v2.9a.7.7 0 01-.7.7H9.8"/>
    <path d="M6.2 13.4H3.3a.7.7 0 01-.7-.7V9.8"/>
  </svg>,
  // --- absence marks --------------------------------------------------------
  // Four silhouettes, deliberately unlike each other at 13px and unlike every icon
  // above, because they have to be separable in greyscale: the rota prints.
  sun: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"><circle cx="8" cy="8" r="2.8"/><path d="M8 1.2v1.7M8 13.1v1.7M14.8 8h-1.7M2.9 8H1.2M12.8 3.2l-1.2 1.2M4.4 11.6l-1.2 1.2M12.8 12.8l-1.2-1.2M4.4 4.4L3.2 3.2"/></svg>,
  thermometer: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"><path d="M9.6 9.5V3.6a1.6 1.6 0 00-3.2 0v5.9a2.9 2.9 0 101.6 2.7"/><path d="M8 6.2v4.1"/></svg>,
  cap: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round"><path d="M8 2.4L1.7 5.5 8 8.6l6.3-3.1L8 2.4z"/><path d="M4.4 7.1v3.2c0 1.1 1.6 2 3.6 2s3.6-.9 3.6-2V7.1"/><path d="M14.3 5.5v3.6"/></svg>,
  // A barred circle. NOT eyeoff, which is a barred eye and reads as "hidden" -- the
  // wrong half of the meaning, since nothing about an absent day is hidden, it just
  // is not a shift.
  slash: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"><circle cx="8" cy="8" r="5.9"/><path d="M3.9 3.9l8.2 8.2"/></svg>,
  // The "no colour" tile's slash: one diagonal alone, at the same 1.6 weight as the
  // brackets so the tile and the drag affordance match.
  diag: <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round"><path d="M4 12L12 4"/></svg>,
};


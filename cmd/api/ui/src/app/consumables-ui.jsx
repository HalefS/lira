/* ════════════════ CONSUMABLES: ICONS + PICKER ════════════════ */
/* Each icon is drawn on a 24x24 grid and rendered by <ConsumableIcon>. The keys
   must match ConsumableIcons in internal/data/consumable_items.go — that list is
   what the server accepts, this one is what the UI can draw. To add an icon, add
   it to both (and to CONSUMABLE_ICON_TONE for its color). */
const CI_D = 'fill="currentColor" fill-opacity=".16"';           // duotone body fill
const CI_DOT = 'fill="currentColor" stroke="none"';              // solid dots / text
const CI_TXT = `${CI_DOT} font-family="'DM Sans',system-ui,sans-serif" font-weight="700" text-anchor="middle" dominant-baseline="central"`;

const CONSUMABLE_ICONS = {
  'battery-aa': `
    <rect x="7.5" y="5" width="9" height="17" rx="2.2" ${CI_D}/>
    <rect x="10.2" y="2" width="3.6" height="3" rx=".8"/>
    <text x="12" y="13.7" transform="rotate(-90 12 13.7)" font-size="6.2" ${CI_TXT}>AA</text>`,
  'battery-aaa': `
    <rect x="8.4" y="5" width="7.2" height="17" rx="2" ${CI_D}/>
    <rect x="10.4" y="2" width="3.2" height="3" rx=".8"/>
    <text x="12" y="13.7" transform="rotate(-90 12 13.7)" font-size="5" ${CI_TXT}>AAA</text>`,
  'battery-9v': `
    <rect x="6" y="7.5" width="12" height="14.5" rx="2.2" ${CI_D}/>
    <circle cx="9.6" cy="5" r="1.7"/><circle cx="14.4" cy="5" r="1.7"/>
    <text x="12" y="15.3" font-size="6.4" ${CI_TXT}>9V</text>`,
  'battery-coin': `
    <circle cx="12" cy="12" r="8.6" ${CI_D}/>
    <circle cx="12" cy="12" r="5.2" stroke-width="1.3"/>
    <path d="M12 9.6v4.8M9.6 12h4.8" stroke-width="1.5"/>`,
  'phone': `
    <path d="M3.5 8.2v-.8C3.5 6.1 4.6 5 5.9 5h12.2c1.3 0 2.4 1.1 2.4 2.4v.8c0 .8-.7 1.5-1.5 1.5h-1.9c-.8 0-1.4-.6-1.4-1.4V7.3H9.5v1c0 .8-.6 1.4-1.4 1.4H5c-.8 0-1.5-.7-1.5-1.5z" ${CI_D}/>
    <path d="M7.2 12.2h9.6l3.4 7.8H3.8z" ${CI_D}/>
    <path d="M9.4 15.2h.01M12 15.2h.01M14.6 15.2h.01M9.4 17.5h.01M12 17.5h.01M14.6 17.5h.01" stroke-width="1.9"/>`,
  'mobile': `
    <rect x="6.8" y="2.5" width="10.4" height="19" rx="2.6" ${CI_D}/>
    <path d="M10.4 5.4h3.2"/><path d="M10.6 18.8h2.8"/>`,
  'tablet': `
    <rect x="4.5" y="2.5" width="15" height="19" rx="2.6" ${CI_D}/>
    <circle cx="12" cy="18.6" r=".9" ${CI_DOT}/>`,
  'remote': `
    <rect x="8" y="2" width="8" height="20" rx="3.2" ${CI_D}/>
    <circle cx="12" cy="6.2" r="1.5" stroke-width="1.5"/>
    <circle cx="10" cy="10.6" r=".95" ${CI_DOT}/><circle cx="14" cy="10.6" r=".95" ${CI_DOT}/>
    <circle cx="10" cy="13.6" r=".95" ${CI_DOT}/><circle cx="14" cy="13.6" r=".95" ${CI_DOT}/>
    <path d="M10.2 17.6h3.6"/>`,
  'tv': `
    <rect x="2.5" y="4.5" width="19" height="13" rx="2.3" ${CI_D}/>
    <path d="M10.6 8.6v4.8l4-2.4z" stroke-width="1.5"/>
    <path d="M7 21l1.2-3.5M17 21l-1.2-3.5"/>`,
  'monitor': `
    <rect x="2.5" y="3.5" width="19" height="13" rx="2.3" ${CI_D}/>
    <path d="M6.5 8h6M6.5 11.2h3.6" stroke-width="1.5"/>
    <path d="M12 16.5V20M8 20.5h8"/>`,
  'hdmi': `
    <path d="M4.5 3.5h15v5.2l-3 3.8h-9l-3-3.8z" ${CI_D}/>
    <path d="M8.3 6.2v2.4M10.8 6.2v2.4M13.2 6.2v2.4M15.7 6.2v2.4" stroke-width="1.5"/>
    <path d="M9 12.5v3.5h6v-3.5"/><path d="M12 16v5.5"/>`,
  'ethernet': `
    <rect x="6" y="3.5" width="12" height="11.5" rx="1.9" ${CI_D}/>
    <path d="M9 6.2v3.6M11.2 6.2v3.6M13.4 6.2v3.6M15.6 6.2v3.6" stroke-width="1.4"/>
    <path d="M9.4 15v3.6h5.2V15"/><path d="M12 18.6v3"/>`,
  'charger': `
    <rect x="6" y="8" width="12" height="9.4" rx="2.6" ${CI_D}/>
    <path d="M9.6 8V4.4M14.4 8V4.4"/>
    <path d="M12 17.4v4"/>`,
  'power-strip': `
    <rect x="2.5" y="7.5" width="19" height="9" rx="2.4" ${CI_D}/>
    <path d="M6.3 10.6v2.8M8.3 10.6v2.8M11 10.6v2.8M13 10.6v2.8M15.7 10.6v2.8M17.7 10.6v2.8" stroke-width="1.5"/>
    <path d="M12 16.5v4"/>`,
  'bulb': `
    <path d="M12 2.5a6.5 6.5 0 0 0-3.9 11.7c.6.5.9 1.2.9 1.9v.4h6v-.4c0-.7.3-1.4.9-1.9A6.5 6.5 0 0 0 12 2.5z" ${CI_D}/>
    <path d="M9.5 19.2h5M10.6 21.6h2.8"/>
    <path d="M10.3 9.6 12 12l1.7-2.4M12 12v4.4" stroke-width="1.4"/>`,
  'key-card': `
    <rect x="2.5" y="5.5" width="19" height="13" rx="2.4" ${CI_D}/>
    <rect x="2.5" y="8.6" width="19" height="2.8" fill="currentColor" fill-opacity=".5" stroke="none"/>
    <path d="M6 15.2h5.5"/>
    <path d="M16.3 13.4a2.4 2.4 0 0 1 0 3.2M18.4 12.2a4.2 4.2 0 0 1 0 5.6" stroke-width="1.5"/>`,
  'lock': `
    <rect x="5" y="10.5" width="14" height="10.5" rx="2.6" ${CI_D}/>
    <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/>
    <circle cx="12" cy="15" r="1.4" ${CI_DOT}/><path d="M12 15.6v2"/>`,
  'router': `
    <rect x="3" y="14" width="18" height="6.5" rx="2.2" ${CI_D}/>
    <path d="M9.6 9.6a3.4 3.4 0 0 1 4.8 0M7.2 7.2a6.8 6.8 0 0 1 9.6 0" stroke-width="1.6"/>
    <circle cx="12" cy="12" r=".9" ${CI_DOT}/>
    <circle cx="6.8" cy="17.2" r=".85" ${CI_DOT}/><circle cx="9.6" cy="17.2" r=".85" ${CI_DOT}/>
    <path d="M14.6 17.2h3.8" stroke-width="1.5"/>`,
  'usb': `
    <rect x="8" y="2.5" width="8" height="6" rx=".9"/>
    <circle cx="10.4" cy="5.5" r=".75" ${CI_DOT}/><circle cx="13.6" cy="5.5" r=".75" ${CI_DOT}/>
    <rect x="6.5" y="8.5" width="11" height="13" rx="2.4" ${CI_D}/>
    <circle cx="12" cy="17.6" r="1.3"/>`,
  'mouse': `
    <rect x="6.5" y="2.5" width="11" height="19" rx="5.5" ${CI_D}/>
    <path d="M6.5 10.4h11M12 2.5v4"/>
    <rect x="11" y="5.4" width="2" height="3.4" rx="1" ${CI_DOT}/>`,
  'keyboard': `
    <rect x="2" y="6" width="20" height="12" rx="2.4" ${CI_D}/>
    <path d="M6 9.8h.01M9 9.8h.01M12 9.8h.01M15 9.8h.01M18 9.8h.01M6 12.6h.01M9 12.6h.01M12 12.6h.01M15 12.6h.01M18 12.6h.01" stroke-width="2.2"/>
    <path d="M7.5 15.4h9" stroke-width="1.6"/>`,
  'headset': `
    <path d="M4.5 14v-2a7.5 7.5 0 0 1 15 0v2"/>
    <rect x="3" y="13" width="4.2" height="6.6" rx="1.7" ${CI_D}/>
    <rect x="16.8" y="13" width="4.2" height="6.6" rx="1.7" ${CI_D}/>
    <path d="M19 19.6v.3c0 1.5-1.2 2.6-2.7 2.6H14"/>
    <circle cx="12.6" cy="22.5" r=".9" ${CI_DOT}/>`,
  'printer': `
    <path d="M7 9V3.5h10V9"/>
    <rect x="3" y="9" width="18" height="8" rx="2.2" ${CI_D}/>
    <rect x="7" y="14" width="10" height="7" rx=".9" ${CI_D}/>
    <path d="M9.6 17h4.8M9.6 19h2.6" stroke-width="1.4"/>
    <circle cx="17.6" cy="11.8" r=".8" ${CI_DOT}/>`,
  'box': `
    <path d="M3.5 7.5 12 3l8.5 4.5v9L12 21l-8.5-4.5z" ${CI_D}/>
    <path d="M3.5 7.5 12 12l8.5-4.5M12 12v9"/>`,
};

const CONSUMABLE_ICON_TONE = {
  'battery-aa':'amber','battery-aaa':'amber','battery-9v':'amber','battery-coin':'amber','bulb':'amber',
  'phone':'blue','mobile':'blue','tablet':'blue','headset':'blue','key-card':'blue','lock':'blue',
  'remote':'teal','tv':'teal','monitor':'teal','printer':'teal',
  'hdmi':'green','ethernet':'green','router':'green','usb':'green','mouse':'green','keyboard':'green',
  'charger':'red','power-strip':'red',
  'box':'gray',
};
const CONSUMABLE_ICON_KEYS = Object.keys(CONSUMABLE_ICONS);
const MAX_CONSUMABLE_QTY = 99;
// Mirrors maxTVSwapsPerIssue in internal/data/tv_swaps.go.
const MAX_TV_SWAPS = 10;

function ConsumableIcon({ name, size=36 }) {
  const key = CONSUMABLE_ICONS[name] ? name : 'box'; // unknown key → generic box
  return (
    <span className={`cicon tone-${CONSUMABLE_ICON_TONE[key]}`}
      style={{width:size, height:size, borderRadius:Math.round(size*0.26)}} aria-hidden="true">
      {/* trusted static markup from CONSUMABLE_ICONS above, never user input */}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7"
        strokeLinecap="round" strokeLinejoin="round" dangerouslySetInnerHTML={{__html: CONSUMABLE_ICONS[key]}}/>
    </span>
  );
}

/* The grid of icons a manager picks from when creating / editing a consumable. */
function ConsumableIconGrid({ value, onChange, labelledBy }) {
  return (
    <div className="icon-grid" role="group" aria-labelledby={labelledBy}>
      {CONSUMABLE_ICON_KEYS.map(k=>(
        <button type="button" key={k} title={k.replace(/-/g,' ')} aria-pressed={value===k}
          className={`icon-opt ${value===k?'selected':''}`} onClick={()=>onChange(k)}>
          <ConsumableIcon name={k} size={30}/>
        </button>
      ))}
    </div>
  );
}

/* Small icons (with a quantity badge) shown in the issue tables. */
/* Renders the TVs moved for an issue: a set icon followed by the room pair,
   reading as "this set came from here and went there". */
function TVSwapStrip({ swaps, t }) {
  if (!swaps || swaps.length === 0) return null;
  return (
    <div className="tp-strip">
      {swaps.map(s=>(
        <span key={s.id} className="tp-strip-item swap"
              title={t.tvSwapTitle + (s.notes ? ' — ' + s.notes : '')}>
          {Ic.tv} <strong>{s.fromRoom}</strong>
          <span className="tp-swap-arrow">{'\u2194'}</span>
          <strong>{s.toRoom}</strong>
        </span>
      ))}
    </div>
  );
}

function ConsumableStrip({ items }) {
  if (!items || items.length === 0) return null;
  return (
    <div className="cstrip">
      {items.map((c,i)=>(
        <span key={i} className="cstrip-item" title={`${c.item} × ${c.quantity}`}>
          <ConsumableIcon name={c.icon} size={22}/>
          {c.quantity > 1 && <span className="cstrip-qty">{c.quantity}</span>}
        </span>
      ))}
    </div>
  );
}

/* The third-party handovers shown read-only under an issue in the tables. One
   pill per handover, so a support call that is still outstanding is visible
   without opening the issue. The name and colour come from the company catalog,
   joined by the server, so a company added in Settings appears here too. */
function SupportRequestStrip({ requests, t }) {
  if (!requests || requests.length === 0) return null;
  return (
    <div className="tp-strip">
      {requests.map(s=>{
        const label  = s.companyName || s.company;
        // The server's summary prefers a tracking reference; the mapped fields
        // cover a handover whose values were recorded under a field the manager
        // has since renamed or removed.
        const values = (s.fields || []).map(f => f.value).filter(Boolean);
        const detail = s.referenceSummary || values.join(' · ') || '';
        const state  = s.status==='solved' ? t.supportSolved : t.supportPending;
        return (
          <span key={s.id} className={`tp-strip-item ${s.companyColor || 'gray'}`} title={`${label} · ${detail || '—'} · ${state}`}>
            {s.status==='solved' ? Ic.check : Ic.clock} {label}{detail ? ` ${detail}` : ''}
          </span>
        );
      })}
    </div>
  );
}


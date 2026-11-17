/* ── Avatar ──
   Shows the member's uploaded photo when they have one, and their initials on
   a colour derived from avatar_idx when they don't. Previously this only ever
   drew initials, so every call site that wanted a photo had to hand-roll an
   img-or-initials ternary — the logic now lives here once.
   `src` and `style` are optional: call sites that only know a name and an index
   (issue lists, the stats charts) keep the initials behaviour. */
function Avatar({ name, idx, size='md', src, style }) {
  const dims = size==='sm' ? {width:24,height:24,fontSize:9} : size==='lg' ? {width:40,height:40,fontSize:15} : {};
  const box  = { ...dims, ...style };
  // alt="" because every current call site already shows the name beside the
  // avatar, and naming it again would have screen readers announce it twice.
  return src
    ? <img className="avatar-circle" src={src} style={box} alt=""/>
    : <div className={`avatar-circle av-${(idx??0)%5}`} style={box}>{initials(name)}</div>;
}

/* ─── Display names ───

   ONE name per account, stored in users.name as free text, and TWO renderings of
   it.

   The rota shows the WHOLE name. Everything else shows the first word and the
   last. The rota is the one screen whose job is to say WHO — a grid of people
   against days, read at a glance, where a repeated "João Santos" and "Bruno
   Santos" are two different people and dropping what sits between them loses
   something real. Every other screen is showing a name as ATTRIBUTION: who logged
   this, who solved it, who is assigned. There the shortest form that still
   identifies the person is the right one.

   FIRST AND LAST, NOT FIRST AND SECOND. "João Silva Santos" becomes "João Santos",
   not "João Silva" — the rule is "name and last surname", and the second word is
   not a surname in general, it is whatever happens to be in the middle.

   A SINGLE WORD IS RETURNED UNCHANGED. There is no surname to keep, and inventing
   one by doubling it — "lira" becoming "lira lira" — would be worse than saying
   nothing at all.

   IT IS THE SAME RULE IN TWO LANGUAGES, and the duplication is deliberate rather
   than convenient: shortName in internal/report/daily.go does this for the daily
   and consumables PDFs, because Go cannot call into the browser. Both take the
   first and last word of a whitespace-split name and nothing else. Change one,
   change the other, or a person's name is spelled two different ways in two
   different documents.

   The attendance workbook is the one export that keeps the whole name: it is a
   signed statement about who worked, and it is the rota's own document.

   TOP LEVEL, which sounds like a note and is not. This function was first placed
   directly beneath Avatar's `return`, which put it INSIDE Avatar's body: after a
   return it is unreachable, and because it was a nested function declaration
   rather than a module-level one, every shortName() call in the file raised
   ReferenceError and the whole application rendered as a blank page. The symptom
   pointed at the call sites -- forty-eight of them, all correct -- and not at the
   one line that was wrong. */
function shortName(name) {
  if (!name) return name;
  const parts = String(name).trim().split(/\s+/).filter(Boolean);
  if (parts.length < 2) return name;
  return parts[0] + ' ' + parts[parts.length - 1];
}

/* ── TypeBadge ── */
// Colors are assigned server-side and stored per issue type as a "#RRGGBB"
// hex value (guaranteed unique — see the admin issue-types catalog).
// TypeColorCtx makes that name -> hex lookup available to any <TypeBadge>
// without having to thread issueTypes through every table component that
// renders one.
const TypeColorCtx = React.createContext({});

function hexToHsl(hex) {
  const r = parseInt(hex.slice(1,3),16)/255, g = parseInt(hex.slice(3,5),16)/255, b = parseInt(hex.slice(5,7),16)/255;
  const max = Math.max(r,g,b), min = Math.min(r,g,b);
  let h, s, l = (max+min)/2;
  if (max === min) { h = s = 0; }
  else {
    const d = max-min;
    s = l > 0.5 ? d/(2-max-min) : d/(max+min);
    switch (max) {
      case r: h = (g-b)/d + (g<b?6:0); break;
      case g: h = (b-r)/d + 2; break;
      default: h = (r-g)/d + 4;
    }
    h /= 6;
  }
  return [h*360, s*100, l*100];
}
function hslToHex(h, s, l) {
  s/=100; l/=100;
  const k = n => (n + h/30) % 12;
  const a = s * Math.min(l, 1-l);
  const f = n => l - a * Math.max(-1, Math.min(k(n)-3, Math.min(9-k(n), 1)));
  const toHex = x => Math.round(255*x).toString(16).padStart(2,'0');
  return `#${toHex(f(0))}${toHex(f(8))}${toHex(f(4))}`;
}
// Turns any "#RRGGBB" hex into a soft badge style — a light tint
// background and a deep, readable text color of the same hue — matching
// the app's existing badge look regardless of which raw color a manager
// picks in the color picker.
function badgeStyleFromHex(hex) {
  if (!/^#[0-9A-Fa-f]{6}$/.test(hex)) return null;
  const [h, s] = hexToHsl(hex);
  const sat = Math.min(s, 70);
  return {
    background: hslToHex(h, sat, 93),
    borderColor: hslToHex(h, sat, 82),
    color: hslToHex(h, Math.min(Math.max(s,40),80), 32),
    borderWidth: 1, borderStyle: 'solid',
  };
}

/* A shift's stored tint, expanded into the custom properties the rota CSS reads.

   The stored value stays one "#rrggbb" and the theme work happens HERE rather than
   in the stylesheet, for the reason badgeStyleFromHex gives: one derivation, used by
   every surface that shows a tint, so a cell and its legend chip cannot disagree
   about what colour a shift is.

     --tint-wash      the cell / legend / picker background. Lightness 92% in light
                      and 17% in dark, which measures 12.4:1 to 14.7:1 against
                      --gray-900 in light and 9.6:1 to 15.0:1 in dark -- a wash that
                      reads on a 44px cell and still carries the cell's ordinary text.
     --tint-dot       the 10px marker on legend chips and picker options. NOT the
                      full-strength hue in dark: that measured 2.16:1 to 2.71:1
                      against its own wash, under the 3:1 a non-text cue owes.
     --tint-ink       ink for a glyph drawn on the raw swatch: white when the swatch
                      is dark enough to carry it, --gray-900 otherwise. All six
                      presets want white; a pale custom yellow wants the other.

   Returns null for no tint, for a malformed hex and for "" -- and the callers spread
   it as style={tintStyle(shift)}, so "no tint" is an ABSENT style and the element
   keeps .rota-cell's own background. That is what makes an untinted shift render
   exactly as it did before, with no conditional CSS anywhere. */
function tintStyle(shift) {
  const hex = (shift && shift.color) || '';
  if (!/^#[0-9A-Fa-f]{6}$/.test(hex)) return null;
  const [h, s] = hexToHsl(hex);
  const sat = Math.min(s, 70);
  return {
    '--tint-wash':     hslToHex(h, sat, 92),
    '--tint-dot':      hslToHex(h, sat, 34),
    '--tint-wash-dark': hslToHex(h, Math.min(s, 56), 17),
    '--tint-dot-dark':  hslToHex(h, sat, 62),
    '--tint-ink':       hexToHsl(hslToHex(h, s, 50))[2] < 62 ? '#FFFFFF' : 'var(--gray-900)',
  };
}

/* Just the two dot custom properties, for the two places that show a colour without
   offering a control. Same derivation, so the two can never disagree.

   Custom PROPERTIES rather than a background, and that is the whole point: an inline
   `background` beats every stylesheet rule, including the html.dark override that
   picks the dot's lightness for a dark card. Written as a background it looked fine
   in light and measured 1.49:1 in dark, because the override never ran. Handing the
   variables over and letting .rota-tint-dot resolve them is what makes the theme
   switch reach this element at all. */
function dotStyle(hex) {
  const s = tintStyle({ color: hex });
  if (!s) return null;
  return { '--tint-dot': s['--tint-dot'], '--tint-dot-dark': s['--tint-dot-dark'] };
}

/* The six preset tints, in picker order: no colour first, then a ladder of hue
   going anti-clockwise from the app's own blue.

   Every one was chosen on four measured constraints rather than on taste:

     white ink on every swatch, 5.11:1 (#0f7d5c) to 6.46:1 (#5b4bc4). One ink
       colour for all six, so the tick glyph inside the chosen tile is #FFFFFF in
       code rather than a per-swatch lookup. This is also why the palette does not
       contain the app's --teal or --green: those are ink tokens tuned for their own
       washes, and a solid swatch needs a darker member of the same hue.

     at least 40 degrees of hue from --orange (h 17.8) and --amber (h 37.4); the
       smallest gap here is 124.6 degrees. Orange is not merely reserved for
       overnight by this list: a tint is a WASH and overnight is a 3px RAIL, so a
       manager who picks orange in the native input gets an orange wash, which still
       cannot read as overnight because the rail is what means overnight.

     pairwise CIE-Lab dE76 at least 25 in normal vision. Measured minimum 29.8.

     pairwise dE76 at least 18 under deuteranopia (Machado 1.0), measured minimum
       19.6. A seventh candidate was cut rather than shipped: a navy that cleared
       every test alone but sat 20.6 from Slate and 20.6 from Blue, and three cool
       neutrals is a difference nobody has to be able to make.

   #5b6779 is deliberately close to --gray-600, because a manager who wants "the
   plain one but tinted" should not have to hunt for a neutral.

   And none of this is load-bearing: the shift name is printed in every cell, in the
   legend and in the picker, so colour is a fourth and last cue everywhere it
   appears. */
const SHIFT_TINTS = ['', '#5b6779', '#1769aa', '#0f7d5c', '#5b4bc4', '#b02e8c', '#be2e4b'];

/* The preset's name in words, which is what makes the control not depend on colour:
   a screen reader hears "Colour for Morning: Indigo", a tooltip reads "Indigo", and
   only the 20px disc is a colour at all. Same idiom as the issue-types swatch. A hex
   the manager picked for itself has no name here, so it falls back to the hex -- a
   worse label than a good name, which is the honest consequence of not having one. */
const shiftTintName = (t, c) => ({
  '':        t.shiftTintNone,
  '#5b6779': t.shiftTintSlate,
  '#1769aa': t.shiftTintBlue,
  '#0f7d5c': t.shiftTintTeal,
  '#5b4bc4': t.shiftTintIndigo,
  '#b02e8c': t.shiftTintMagenta,
  '#be2e4b': t.shiftTintCrimson,
}[c] || (c || '').toUpperCase());

function TypeBadge({ type }) {
  const colors = React.useContext(TypeColorCtx);
  const hex = colors[type];
  const style = hex ? badgeStyleFromHex(hex) : null;
  return <span className="badge" style={style || undefined}>{type}</span>;
}


/* Every table that scrolls sideways does it through one wrapper, so there is one
   place to give it a keyboard route in and a name to announce. The label is the
   table's own visible heading, passed in by the caller. */
function TableScroll({ label, children }) {
  return (
    <div className="table-scroll" role="region" aria-label={label} tabIndex={0}>
      {children}
    </div>
  );
}

/* ── Toast ──
   The wrapper is rendered whether or not a message is present, and that is the
   whole mechanism: a live region that appears at the same moment as its text is
   announced unreliably, because the screen reader never saw the region become
   non-empty. Keeping it in the document and swapping its contents means every
   toast is an insertion into a region that was already being watched.

   aria-atomic="true" reads the message as one sentence rather than announcing
   the dot and the text as two separate changes. Hover and focus hold the
   dismissal, so a message that arrives while someone is already reading it does
   not vanish mid-line. */
function Toast({ msg, onDone }) {
  const [held, setHeld] = useState(false);
  useEffect(() => {
    if (!msg || held) return;
    const t = setTimeout(onDone, 2800);
    return ()=>clearTimeout(t);
  }, [msg, held]);
  return (
    <div className="toast-region" role="status" aria-live="polite" aria-atomic="true"
         onMouseEnter={()=>setHeld(true)} onMouseLeave={()=>setHeld(false)}
         onFocus={()=>setHeld(true)} onBlur={()=>setHeld(false)}>
      {msg && <div className="toast"><span className="toast-dot" aria-hidden="true"></span>{msg}</div>}
    </div>
  );
}

/* ── LangToggle ── */
function LangToggle({ lang, onChange }) {
  return (
    <div className="lang-toggle" title="Language / Idioma">
      <button className={`lang-btn ${lang==='en'?'active':''}`} onClick={()=>onChange('en')}>
        <span className="lang-flag">🇬🇧</span> EN
      </button>
      <button className={`lang-btn ${lang==='pt'?'active':''}`} onClick={()=>onChange('pt')}>
        <span className="lang-flag">🇵🇹</span> PT
      </button>
    </div>
  );
}

/* ── ThemeToggle ── */
function ThemeToggle({ theme, onChange }) {
  const isDark = theme === 'dark';
  return (
    <button
      className="theme-btn"
      onClick={() => onChange(isDark ? 'light' : 'dark')}
      title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
    >
      {isDark ? (
        <svg className="theme-icon-sun" viewBox="0 0 20 20" fill="currentColor">
          <circle cx="10" cy="10" r="4"/>
          <path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.22 4.22l1.42 1.42M14.36 14.36l1.42 1.42M4.22 15.78l1.42-1.42M14.36 5.64l1.42-1.42" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" fill="none"/>
        </svg>
      ) : (
        <svg className="theme-icon-moon" viewBox="0 0 20 20" fill="currentColor">
          <path d="M17.293 13.293A8 8 0 016.707 2.707a8.001 8.001 0 1010.586 10.586z"/>
        </svg>
      )}
    </button>
  );
}


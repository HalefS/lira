function mondayISO(d) {
  const x = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  x.setDate(x.getDate() - ((x.getDay() + 6) % 7));
  return localISODate(x);
}
function shiftISO(iso, n) {
  const [y, m, d] = iso.split('-').map(Number);
  return localISODate(new Date(y, m - 1, d + n));
}
/* How many calendar days an inclusive [from, to] span covers, or 0 if it is not a
span at all.

   UTC throughout, and Date.UTC rather than dividing a duration by 24h -- the server's
Absence.DayCount says the same thing for the same reason: two LOCAL midnights are 23
or 25 hours apart in real time across a DST change, and a division then loses a day
twice a year. The form needs this to tell a manager how much they have just booked
while they are looking at a Monday-to-Sunday grid, which is exactly the range where a
DST boundary can fall inside it. */
function absenceDayCount(fromISO, toISO) {
  if (!fromISO || !toISO || toISO < fromISO) return 0;
  const [fy, fm, fd] = fromISO.split('-').map(Number);
  const [ty, tm, td] = toISO.split('-').map(Number);
  return Math.round((Date.UTC(ty, tm - 1, td) - Date.UTC(fy, fm - 1, fd)) / 86400000) + 1;
}

function isoToDate(iso) {
  const [y, m, d] = iso.split('-').map(Number);
  return new Date(y, m - 1, d);
}
function shiftMinutes(s) {
  if (!s) return 0;
  const mins = t => {
    const [h, m] = String(t || '0:00').split(':').map(Number);
    return (h || 0) * 60 + (m || 0);
  };
  const raw = mins(s.end_time) - mins(s.start_time);
  return raw > 0 ? raw : raw + 1440;
}
function rotaWeekHours(member) {
  return (member.days || []).reduce((n, s) => n + shiftMinutes(s), 0);
}

/* The four reasons a person can be off on a date, and what each is allowed to look
   like.

   Order is how much each is allowed to disclose, which is not the order the schema
   lists them in and is the reason this is a list here rather than an enum.

   An absence is NOT a shift and never renders as one: no name, no hours, no rail. It
   is a hatch, a rule, a glyph and a word. Two absences may cover one date -- the
   server allows it, because sick leave inside a booked holiday is a real thing -- so
   absenceAt returns the FIRST and the cell shows that one.

   `--hue` is the per-kind accent. vacation is violet, which appears nowhere else in
   this application, so a reader who learns it once keeps it. away is a structural
   grey, deliberately: a generic absence is meant to be unremarkable, and grey is a
   colour no manager can pick as a shift tint. */
const ABSENCE_KINDS = [
  { id: 'vacation',    hue: 'var(--violet)' },
  { id: 'sick',        hue: 'var(--teal)' },
  { id: 'training',    hue: 'var(--green)' },
  { id: 'unavailable', hue: 'var(--gray-600)' },
];
const absenceKind = id => ABSENCE_KINDS.find(k => k.id === id) || ABSENCE_KINDS[3];
const absenceHue  = id => absenceKind(id).hue;
const absenceGlyph = {
  vacation: Ic.sun, sick: Ic.thermometer, training: Ic.cap, unavailable: Ic.slash,
};

/* The absence covering one person on one date, or null.

   `list` is already that person's absences -- SchedulePage groups the flat
   GET /v1/schedule array by user_id once rather than rescanning it per cell.

   Comparisons are string comparisons on "YYYY-MM-DD", which is what the server
   marshals JSONDate to and what week.days[i].date already carries. No Date objects
   anywhere in here, so there is no timezone to get wrong -- and this is exactly the
   trap the server-side Absences.Covers comment describes, where a `date` column
   scanned from Postgres is midnight UTC and the week's dates are local midnight.

   A run is a RANGE rather than seven per-week cells, and that is what lets this
   answer a question the per-week shape cannot: whether the absence carries on past
   the end of the week on screen. See the continuation marker in RotaTable. */
function absenceAt(list, iso) {
  if (!list || !iso) return null;
  for (let i = 0; i < list.length; i++) {
    const a = list[i];
    if (a.starts_on <= iso && iso <= a.ends_on) return a;
  }
  return null;
}

/* The hours that will ACTUALLY be worked this week.

   rotaWeekHours stays exactly as it is and stays the standing total: it is what the
   recurring rota says, and it is the number that has to survive an absence ending.
   This is the other number, and it is the one a manager is reconciling a week
   against -- somebody whose Wednesday is HOLIDAY is not working Wednesday's eight
   hours this week, and being told "16 h/week" for them is the one control that
   exists to make an uneven rota visible reporting it wrong.

   The two differ only when an absence covers a day that had a shift, so a rota with
   no absences renders identically to before. */
function rotaWorkedMinutes(member, week, absencesByUser) {
  const list = absencesByUser && absencesByUser.get(member.user_id);
  return (member.days || []).reduce((n, s, i) => {
    const d = week && week.days && week.days[i];
    if (d && absenceAt(list, d.date)) return n;
    return n + shiftMinutes(s);
  }, 0);
}

/* Grouped once per render rather than per cell: 42 cells times a scan of the whole
   absence list is 42 scans, and the grouping is also what lets a member be absent
   for a whole week be answered in O(1) per cell. */
function absencesByUser(absences) {
  const map = new Map();
  for (const a of (absences || [])) {
    if (!map.has(a.user_id)) map.set(a.user_id, []);
    map.get(a.user_id).push(a);
  }
  return map;
}

/* Where a fixed-position popover goes, and when it moves.

   ONE function, called on open, on scroll and on resize, so the three cannot disagree
   about which side the panel is on -- the failure mode when two of them compute
   placement independently is a panel that is correctly placed on open and wrong a
   second later.

   capture:true on the scroll listener because .content is the scroll container and
   scroll events do not bubble to window.

   The panel is position:fixed rather than absolutely positioned inside the cell, and
   NOT rendered through createPortal. A fixed box is positioned against the viewport and
   is not clipped by ancestor overflow, which matters because the cell sits under five
   clipping ancestors:

       .rota-table-wrap   overflow-x: auto     (the horizontal scroller)
       .table-card        overflow: hidden
       .content           overflow: auto      (the vertical scroller)
       .main              overflow: hidden
       .app               overflow: hidden

   Overflow clipping beats any z-index, so the panel's z-index:30 achieved nothing
   against .table-card and it was cut off at the card's edge on the bottom rows --
   measured 119px past the card at row 4 and 174px at row 5, on a 890px viewport.

   Why not createPortal, since react-dom exports it: it would break option clicks on
   arrival. The outside-click handler is
   `wrapRef.current.contains(e.target)`, and options carry onMouseDown={e => e.preventDefault()}
   -- which stops focus moving, not propagation. With the panel in document.body,
   wrapRef.contains(option) is false, so mousedown on an option closes the panel before
   the click fires and the button is already unmounted. Picking a shift would silently
   do nothing. A portal would turn a one-line CSS problem into a rewrite of the
   dismissal contract, and would also have to re-thread id and aria-controls across the
   DOM boundary. Keeping the panel a child of .rota-picker leaves all of that untouched.

   THE CONSTRAINT THAT MAKES position:fixed WORK, and the reason it is worth grepping
   for: an ancestor only re-captures a fixed descendant if it establishes a containing
   block, which means transform, perspective, filter, backdrop-filter, will-change
   (any of those four), contain: paint/layout/strict, or content-visibility. None of
   .rota-table-wrap, .table-card, .content, .main, .app or #root declares any of them
   today. Adding one -- an entry animation on .content is the obvious accident --
   silently puts this panel back inside the clip, and it fails as "the panel is cut
   off" rather than as an error. */

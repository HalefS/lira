const ADMIN_PREVIEW_ROWS = 5;

/* Rooms get their own preview length, twice the other catalogs' five.

   A room run is much shorter than a catalog entry -- "1401-1412", a handful of
   characters -- so a list of five of them is a couple of lines of screen. Rooms
   are also added in floors, which means the list grows in visible steps and a
   manager checking whether a floor went in wants to see it without a click. Ten
   is still short enough that the load-more button stays meaningful on a hotel
   with many floors, which is the case it exists for. */
const ROOMS_PREVIEW_ROWS = 10;

// Rendered only while something is actually hidden, so a short list carries no
// button and no ruled footer. The number in brackets is what the click buys, which
// is the part that makes it safe to press without opening a dialog to find out.
function AdminShowMore({ hidden, label, onClick }) {
  if (!hidden) return null;
  return (
    <div className="table-footer">
      <button type="button" className="btn btn-ghost" onClick={onClick}>
        {label} ({hidden})
      </button>
    </div>
  );
}

/* Which Settings groups are open, remembered between visits.
   Re-deriving it every visit is part of what made the page feel unpredictable: a
   manager who had closed Departments to get at Rooms found Departments open
   again the next time they came looking, and had to put it back. Only a manager
   ever reaches this page, so there is no per-account state to keep apart, and a
   read that throws -- private browsing, storage disabled -- falls back to the
   caller's default rather than taking the page down with it. */
const SET_OPEN_KEY = 'lira_set_open_';
function setGroupPrefOpen(id, fallback) {
  try {
    const raw = localStorage.getItem(SET_OPEN_KEY + id);
    return raw === null ? fallback : raw === '1';
  } catch (e) { return fallback; }
}
function rememberSetGroupOpen(id, open) {
  try { localStorage.setItem(SET_OPEN_KEY + id, open ? '1' : '0'); } catch (e) { /* nothing to do */ }
}

/* One collapsible group, used at both levels of the page.
   The header is a single <button> rather than a heading with a toggle button
   beside it: the native element brings Enter, Space, the button role and a focus
   ring without any of it being reimplemented, and the whole 44px row is one tab
   stop instead of two. aria-expanded is what tells a screen reader whether the
   region below is there at all, which is the only thing a collapsed group needs
   announced -- hence no role="region" per group, which would put seven more
   landmarks in the way of someone trying to skim the page. */
function SetGroup({ id, icon, title, sub, count, level, defaultOpen, children }) {
  const [open, setOpen] = useState(() => setGroupPrefOpen(id, !!defaultOpen));
  const inner = useRef(null);

  // A collapsed group keeps its children in the DOM -- that is what lets the
  // height animate -- so its inputs would otherwise still take focus and a
  // keyboard user could tab into a panel they cannot see. inert drops the subtree
  // out of the tab order and out of the accessibility tree without removing the
  // node, so the transition still has something to measure. Set as a DOM property
  // rather than a JSX attribute because React 18 has no `inert` in its property
  // list and would render inert="true", which does nothing.
  useEffect(() => { if (inner.current) inner.current.inert = !open; }, [open]);

  const toggle = () => {
    const next = !open;
    setOpen(next);
    rememberSetGroupOpen(id, next);
  };

  const Heading = level === 1 ? 'h3' : 'h2';
  return (
    /* The id goes on the SECTION, not only in the aria-controls string below.

       SettingsPage's focusGroup effect finds its target with
       document.getElementById(focusGroup), so a group whose id exists only inside
       `id + '-body'` is invisible to it: the effect returns early and opens nothing.
       That silently broke every cross-page link into a Settings group -- including the
       rota's own "Define shifts" empty-state button, whose entire purpose is to land a
       manager on the shift catalog. Nothing errored and the destination page was right,
       which is why it went unnoticed: correct page, wrong target.

       The section is the element that has to be found, because that is what the effect
       walks up from and what scrollIntoView scrolls to. */
    <section id={id}
      className={'set-group' + (open ? ' open' : '') + (level === 1 ? ' set-group--sub' : '')}>
      <Heading style={{margin:0}}>
        <button type="button" className="set-group-head"
          aria-expanded={open} aria-controls={id + '-body'} onClick={toggle}>
          {icon && <span className="set-group-icon" aria-hidden="true">{icon}</span>}
          <span className="set-group-label">
            <span className="set-group-title">{title}</span>
            {sub && <span className="set-group-sub">{sub}</span>}
          </span>
          <span className="set-group-spacer" />
          {/* count != null, not count > 0: an empty catalog still has to say so
              on its collapsed header, or "empty" is indistinguishable from
              "not loaded". */}
          {count != null && <span className="set-group-count">{count}</span>}
          <span className="set-group-chev" aria-hidden="true">{Ic.chevron}</span>
        </button>
      </Heading>
      <div className="set-group-body" id={id + '-body'}>
        <div className="set-group-inner" ref={inner}>{children}</div>
      </div>
    </section>
  );
}

/* -- Weekly rota ----------------------------------------------------------
   A standing weekly pattern: who is on which shift on which weekday. The same
   seven answers every week until somebody changes them, which is why the week
   navigation below moves dates rather than data.

   Helpers first. Four of them, and three exist only because of the recurring
   design:

   - mondayISO: the week this date falls in. Built on the local calendar rather
     than toISOString(), for the reason the comment beside localISODate gives --
     a local Monday in a timezone west of UTC formats through toISOString as the
     Sunday before it, which here would make "next week" a dead button.
   - shiftISO: a day offset from a week anchor.
   - shiftMinutes: how long a shift occupies on the day it starts. The wrap is
     what makes 22:00-06:00 count as 8 hours rather than -16. end === start is
     refused by the server and never arrives, but the 0-to-1440 fallback keeps a
     malformed row from printing a negative week.
   - rotaWeekHours: the standing week's total for one member, which is the number
     that makes an uneven rota visible before anybody reads a word.
   The client computes the total rather than being told it because the server
   sends durations nowhere: Shift has no minutes field, and adding one would mean
   two places computing the same thing. */

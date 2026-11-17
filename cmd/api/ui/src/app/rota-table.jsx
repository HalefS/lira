function RotaTable({ t, week, members, shifts, absences, editing, draft, setCell,
                      onAddAbsence, onEditAbsence, onRemoveAbsence }) {
  const shown = editing ? (draft || []) : members;
  const gridRef = useRef(null);

  // Grouped once per render rather than per cell, and the same map answers both the
  // per-day lookup and the "is this person away all week" question below.
  const absByUser = useMemo(() => absencesByUser(absences), [absences]);
  // ---- Range selection ------------------------------------------------------
  //
  // One person's run of days, inside one row. `anchor` is the cell the run grew FROM
  // and never moves; `from`/`to` are its bounds, always ordered.
  //
  // The anchor is in the state rather than re-derived from the focused cell, and that
  // is the whole design. Without it, Shift+ArrowLeft has to work out which end of the
  // range is "towards the key" from where the focus happens to be, and gets it wrong
  // the moment the range was made by dragging right-to-left -- at which point holding
  // the key walks the selection back and forth over itself.
  const [sel, setSel] = useState(null);
  // Which cell's picker to open, plus a nonce so the SAME cell can be re-opened.
  //
  // A drag cannot open a picker by itself. Once the pointer is captured, the click the
  // browser synthesises after pointerup is dispatched to the captured element -- the
  // cell the drag started on -- or to the nearest common ancestor of the two cells,
  // which is the <tr>. Neither is the cell the manager let go over, so the panel would
  // open in the wrong place or not at all. Hence an explicit instruction, and hence the
  // nonce: a boolean would see "still open on this cell" and do nothing.
  const [openAt, setOpenAt] = useState(null);
  const dragRef = useRef(null);
  const swallowRef = useRef(false);

  // Where a pointer is over the grid, as a day index in THIS row, or -1.
  //
  // Hit-tested rather than derived from column geometry: the grid scrolls horizontally
  // inside .rota-table-wrap, the columns are not equal width, and a geometry answer has
  // to know about the scroll offset -- which is a second implementation of the layout,
  // and a second thing to keep correct when the layout changes. What the browser says
  // a point is over IS the layout.
  //
  // Same ROW only, and that check is the feature's boundary rather than a defensive
  // guard: a run crossing into another person would break the save bar's count, which
  // counts people, and would make one gesture write two rows.
  const dayAt = (x, y, tr) => {
    const el = document.elementFromPoint(x, y);
    if (!el || !el.closest) return -1;
    const td = el.closest('td[data-rota-i]');
    if (!td || td.parentElement !== tr) return -1;
    const n = Number(td.getAttribute('data-rota-i'));
    return Number.isInteger(n) ? n : -1;
  };

  const onDayPointerDown = (e, m, i) => {
    if (!editing) return;
    if (e.button !== undefined && e.button !== 0) return;   // right-click is not a drag
    dragRef.current = {
      userId: m.user_id, i, tr: e.currentTarget.parentElement,
      x: e.clientX, y: e.clientY, pointerId: e.pointerId,
      dragging: false, last: i,
    };
    // Deliberately NO capturePointer yet. Below the threshold this gesture has to be
    // indistinguishable from a plain click, and capturing on pointerdown changes which
    // element the click is dispatched to -- which is the mechanism the plain click
    // depends on. Capture is claimed by the first move that commits to being a drag.
  };

  const onDayPointerMove = (e, m) => {
    const d = dragRef.current;
    if (!d || d.pointerId !== e.pointerId) return;

    if (!d.dragging) {
      // 6px, as a straight-line distance. A click is a press and a release a pixel or
      // two apart, and a trackpad's resting tremor is routinely more than the 1px that
      // would read as "obviously not a drag" -- but 6px is still a small fraction of a
      // cell's width, so a manager who means to drag never has to leave the cell they
      // started in to commit to it.
      if (Math.hypot(e.clientX - d.x, e.clientY - d.y) < 6) return;
      d.dragging = true;
      try { e.currentTarget.setPointerCapture(d.pointerId); } catch (_) {}
      setSel({ userId: d.userId, anchor: d.i, from: d.i, to: d.i });
    }

    const j = dayAt(e.clientX, e.clientY, d.tr);
    if (j < 0) return;          // off the row: the run holds rather than collapsing
    d.last = j;
    const lo = Math.min(d.i, j), hi = Math.max(d.i, j);
    setSel(s => (s && s.from === lo && s.to === hi) ? s
                                             : { userId: d.userId, anchor: d.i, from: lo, to: hi });
  };

  const onDayPointerUp = (e, m) => {
    const d = dragRef.current;
    if (!d || d.pointerId !== e.pointerId) return;
    dragRef.current = null;
    if (!d.dragging) return;        // a click, not a drag. The button's handler owns it.
    try { e.currentTarget.releasePointerCapture(d.pointerId); } catch (_) {}

    // Swallow the click the browser is about to synthesise: with the pointer captured
    // on the cell the drag started on, that click opens the ANCHOR's picker, right
    // beside the one about to open at the far end. Cleared by the click itself; the
    // timeout is for the drags that end WITHOUT one -- released outside the window,
    // pointer cancelled -- which would otherwise eat the manager's next click.
    swallowRef.current = true;
    setTimeout(() => { swallowRef.current = false; }, 0);

    // Open at the far end, where the eye already is -- unless that day is an absence.
    // An absent cell's panel offers Edit and Remove and no shift at all, so a run that
    // happened to finish on one would be unappliable. The first day in the run that CAN
    // take a shift gets the panel instead, which is the only case where the panel does
    // not open under the pointer.
    const list = absByUser.get(d.userId) || [];
    const lo = Math.min(d.i, d.last), hi = Math.max(d.i, d.last);
    let target = absenceAt(list, week.days[d.last].date) ? -1 : d.last;
    if (target < 0) {
      for (let k = lo; k <= hi; k++) {
        if (!absenceAt(list, week.days[k].date)) { target = k; break; }
      }
    }
    if (target < 0) return;        // the whole run is absence: there is nothing to set
    setOpenAt(a => ({ userId: d.userId, i: target, nonce: (a ? a.nonce : 0) + 1 }));
  };

  const onGridClickCapture = e => {
    if (!swallowRef.current) return;
    swallowRef.current = false;
    // Capture phase, so this runs before the cell button's own onClick. stopPropagation
    // from a capture handler stops the rest of React's dispatch tree, which is what
    // keeps the anchor's picker from opening underneath the one we just asked for.
    e.stopPropagation();
    e.preventDefault();
  };

  const onDayKeyDown = (e, m, i) => {
    if (!editing) return;

    if (e.key === 'Escape') {
      if (sel && sel.userId === m.user_id) { setSel(null); e.preventDefault(); }
      return;
    }
    if (!e.shiftKey) return;
    const left = e.key === 'ArrowLeft', right = e.key === 'ArrowRight';
    const home = e.key === 'Home', end = e.key === 'End';
    if (!(left || right || home || end)) return;

    // aria-expanded rather than a prop: the panel's open state is the truth, and it is
    // already published on the control for assistive technology. Reading it back means
    // the two can never disagree.
    const cell = e.target && e.target.closest && e.target.closest('.rota-cell');
    if (cell && cell.getAttribute('aria-expanded') === 'true') return;

    const n = week.days.length;
    const mine = sel && sel.userId === m.user_id ? sel : null;
    const anchor = mine ? mine.anchor : i;
    let from, to;
    if (home) {
      from = 0; to = anchor;
    } else if (end) {
      from = anchor; to = n - 1;
    } else if (left) {
      // Grow leftward: the left edge moves one further out, the right edge never comes
      // back in. That is what makes holding the key widen the run instead of shrinking
      // it and bouncing off the anchor.
      from = Math.max(0, (mine ? mine.from : anchor) - 1);
      to = Math.max(anchor, mine ? mine.to : anchor);
    } else {
      from = Math.min(anchor, mine ? mine.from : anchor);
      to = Math.min(n - 1, (mine ? mine.to : anchor) + 1);
    }
    setSel({ userId: m.user_id, anchor, from, to });
    // Focus stays on the anchor. The cell being extended FROM is the one the manager is
    // looking at, and moving focus would put the focus ring at the far end of a run
    // whose whole point is being able to see where it started.
    e.preventDefault();
  };

  const inSel = (m, i) => !!(sel && sel.userId === m.user_id && i >= sel.from && i <= sel.to);

  // What the panel needs to say about the run: how many days it covers, and how many of
  // them will actually change.
  const rangeFor = (m, i) => {
    if (!inSel(m, i) || !sel) return null;
    const list = absByUser.get(m.user_id) || [];
    let applicable = 0;
    for (let k = sel.from; k <= sel.to; k++) {
      if (!absenceAt(list, week.days[k].date)) applicable++;
    }
    return { count: sel.to - sel.from + 1, applicable };
  };

  const applyPick = (m, i, s) => {
    const r = sel && sel.userId === m.user_id && i >= sel.from && i <= sel.to ? sel : null;
    setSel(null);
    if (!r) { setCell(m.user_id, i, s); return; }
    const list = absByUser.get(m.user_id) || [];
    for (let k = r.from; k <= r.to; k++) {
      // Absent days inside a run are SKIPPED, not refused. One sick day must not stop a
      // manager setting the other six, and the panel says how many of the N will
      // change -- so the skip is stated in advance rather than discovered afterwards.
      if (!absenceAt(list, week.days[k].date)) setCell(m.user_id, k, s);
    }
  };

  // A run belongs to one week. Changing week or leaving edit mode drops it, rather than
  // leaving a selection painted on cells it no longer describes.
  useEffect(() => { setSel(null); setOpenAt(null); }, [week.start, editing]);

  // Entering edit mode hands focus to the grid's first real cell. On the way out
  // it is left alone: the segmented control is still on screen, so a Tab from
  // wherever the focus ended up still lands somewhere sensible.
  useEffect(() => {
    if (!editing) return;
    const first = gridRef.current && gridRef.current.querySelector('.rota-cell.is-editable');
    if (first) first.focus();
  }, [editing]);

  const peopleOn = shiftId => members.filter(m => (m.days || []).some(d => d && d.id === shiftId)).length;

  return (
    <div className="rota-table-wrap" ref={gridRef} onClickCapture={onGridClickCapture}>
      <table className="rota-table">
        <caption className="sr-only">
          {t.rotaTitle} &mdash; {week.start} to {week.end}
        </caption>
        <thead>
          <tr>
            <th scope="col" className="rota-name-head">{t.rotaNameColumn}</th>
            {week.days.map((d, i) => (
              <th key={d.weekday} scope="col"
                className={d.is_today ? 'rota-headcol-today' : undefined}
                aria-current={d.is_today ? 'date' : undefined}>
                {t.rotaDays[d.weekday - 1] || d.label}
                <span className="rota-daynum">{d.date.slice(8)}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {shown.map(m => {
            const list = absByUser.get(m.user_id) || [];
            const mins = rotaWeekHours(m);
            // What will actually be worked, which is not what the recurring rota says
            // once an absence covers a day that had a shift.
            const worked = rotaWorkedMinutes(m, week, absByUser);
            const awayMins = mins - worked;
            const free = !(m.days || []).some(Boolean);

            // "Not on this rota" and "away all week" are DIFFERENT statements and must
            // never be the same pill. The stored week of somebody on holiday all week
            // is usually all-null, so the plain `free` test calls them "not on this
            // rota" -- which is false. They are on the rota. They are away. That is
            // exactly the confusion this feature exists to prevent, arriving through
            // the back door.
            const awayAll = free && week.days.every(d => absenceAt(list, d.date));
            const freeLabel = awayAll ? t.rotaFreeAway : t.rotaFreeWeek;

            return (
              <tr key={m.user_id} className={m.active ? undefined : 'rota-inactive'}>
                <th scope="row" className="rota-name-cell">
                  <div className="rota-member">
                    <Avatar name={m.name} idx={m.avatar_idx} size="sm"/>
                    <span className="rota-member-text">
                      <span className="rota-member-name">{m.name}</span>
                      {free
                        ? <span className={'rota-free' + (awayAll ? ' is-away' : '')}>
                            {awayAll && <span aria-hidden="true">{Ic.slash}</span>}
                            {freeLabel}
                          </span>
                        : <>
                            <div className="rota-member-hours">
                              {t.rotaHoursLabel.replace('{hours}', (worked / 60).toFixed(worked % 60 ? 1 : 0))}
                            </div>
                            {/* The hours lost to an absence, named on its own line. A
                                struck-through standing total would be two numbers in one
                                column, and this grid has exactly one column for totals. */}
                            {awayMins > 0 && <span className="rota-away-hours">
                              {Ic.slash}
                              {t.rotaAbsHoursAway.replace('{hours}', (awayMins / 60).toFixed(awayMins % 60 ? 1 : 0))}
                            </span>}
                          </>}
                    </span>
                  </div>
                  {/* How many days the run covers, on the row it belongs to. The name
                      cell is the one place in this row with room for words, and a
                      count floating off to the side would be describing a gesture
                      rather than a person. */}
                  {sel && sel.userId === m.user_id && (
                    <span className="rota-sel-count">
                      {sel.to - sel.from + 1 === 1
                        ? t.rotaSelOneDay
                        : t.rotaSelDays.replace('{count}', sel.to - sel.from + 1)}
                    </span>
                  )}
                </th>
                {week.days.map((d, i) => {
                  const shift = (m.days || [])[i] || null;
                  // The absence covering this date, if any. An absence BEATS the
                  // recurring shift for that day, and -- the point -- the shift is
                  // not erased: Days[i] stays exactly as the pattern says it, so a
                  // manager can still see what the rota has them on that weekday and
                  // move it when the absence ends. Precedence is not deletion.
                  const absence = absenceAt(list, d.date);
                  const cellId = 'rota-u' + m.user_id + '-d' + (i + 1);
                  // "Nobody on this rota" and "a day off" both print a dash, and
                  // aria-label is what tells them apart -- the row badge already
                  // carries the first, so the cell only has to carry the second.
                  const label = shift
                    ? t.rotaCellLabel.replace('{name}', m.name).replace('{shift}', shift.name)
                    : (editing
                        ? t.rotaCellLabelUnset.replace('{name}', m.name)
                        : t.rotaCellLabelOff.replace('{name}', m.name));
                  return (
                    <td key={d.weekday}
                      data-rota-i={i}
                      className={'rota-day' + (d.is_today ? ' rota-col-today' : '')}
                      /* On the <td> rather than on the button. The button's own click
                         and focus behaviour is what a plain click depends on, and
                         wrapping it in gesture handlers is how that gets broken. The td
                         sees the same events by bubbling, and once the pointer is
                         captured it keeps seeing them after the pointer leaves it. */
                      onPointerDown={editing ? e => onDayPointerDown(e, m, i) : undefined}
                      onPointerMove={editing ? e => onDayPointerMove(e, m) : undefined}
                      onPointerUp={editing ? e => onDayPointerUp(e, m) : undefined}
                      onKeyDown={editing ? e => onDayKeyDown(e, m, i) : undefined}>
                      {editing
                        ? <RotaPicker
                          cellId={cellId}
                          value={shift}
                          shifts={shifts}
                          /* The absence covering this date, if any. It goes to the
                             picker rather than being handled around it, because the
                             picker IS the cell's control, and a cell whose control
                             cannot express its state is a lie. */
                          absence={absenceAt(list, d.date)}
                          onAddAbsence={kind => onAddAbsence(m.user_id, m.name, kind, d.date)}
                          onEditAbsence={onEditAbsence}
                          onRemoveAbsence={onRemoveAbsence}
                          memberName={m.name}
                          onPick={s => applyPick(m, i, s)}
                          /* The run, as the panel needs it: how many days it covers,
                             and how many of them can take a shift. Null on every cell
                             outside the run, so the panel says nothing about a
                             selection that is not about it. */
                          range={rangeFor(m, i)}
                          selected={inSel(m, i)}
                          selEnd={!!sel && sel.userId === m.user_id && (i === sel.from || i === sel.to)}
                          autoOpen={openAt && openAt.userId === m.user_id && openAt.i === i
                            ? openAt.nonce : 0}
                          t={t}
                          // No positioning props. They used to be alignRight={i >= 3}
                          // and openUp={false}, and every claim in the comment that
                          // justified them is now false: the panel is fixed and clamped
                          // to the viewport by useAnchoredPanel, which knows the room
                          // that actually exists rather than a column index measured at
                          // one window size. A stale confident explanation is worse
                          // than none, so it went with the props.
                          />
                        : absence
                          ? <div className="rota-cell is-away"
                              style={{ '--abs-ink': absenceHue(absence.kind) }}>
                              <span className="rota-cell-hatch" aria-hidden="true"/>
                              <span className="rota-cell-kind" aria-hidden="true">
                                {absenceGlyph[absence.kind]}
                              </span>
                              {/* The word, in caps by CSS. This is the WCAG 1.4.1
                                  answer: the hatch, the rule and the colour are all
                                  colour or texture, and the word is the part that
                                  survives neither. */}
                              <span className="rota-cell-absword">
                                {t.rotaAbsShort[absence.kind]}
                              </span>
                              {/* "And it carries on past Sunday." Only on the run's last
                                  covered cell of the week, and only when the run really
                                  does continue. This is the one piece of absence state
                                  that cannot come out of a per-week array of cells,
                                  and it is why the server sends ranges. */}
                              {absence.ends_on > week.end && d.date === week.end &&
                                <span className="rota-cell-then" aria-hidden="true">{Ic.chevron}</span>}
                              <span className="sr-only">
                                {t.rotaCellLabelAway
                                  .replace('{name}', m.name)
                                  .replace('{kind}', t.rotaAbsKind[absence.kind])}
                              </span>
                            </div>
                        : <div
                            className={'rota-cell' + (shift ? '' : ' is-off') +
                              (shift && shift.overnight ? ' is-night' : '') +
                              (shift && shift.color ? ' is-tinted' : '')}
                            /* The view-mode cell carries the same wash as the edit-mode
                               one. Found by looking at the page: the edit-mode button
                               was tinted and the read-only cell beside it was not, so
                               the grid changed colour the moment a manager pressed
                               Edit. Two renderings of one fact have to be told apart
                               explicitly, which is what this comment is for. */
                            style={tintStyle(shift)}>
                            {shift
                              ? <>
                                  <span className="rota-cell-name">{shift.name}</span>
                                  {shift.overnight && <span className="rota-cell-plus1">+1</span>}
                                </>
                              : <span aria-hidden="true">&mdash;</span>}
                            <span className="sr-only">{label}</span>
                          </div>}
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/* The shift templates card, for the Settings page.

   Shaped like the other catalogs SettingsPage holds -- a .set-form on the left,
   the list on the right -- so it reads as one of them rather than as a new kind
   of thing. Only the rows needed three things the catalogs did not: two time
   fields side by side, an in-place edit holding three controls in one row, and
   the night marker surviving at 12.5px.

   The whole card is manager-only, so there is no can_edit branch inside it. The
   page that holds it already refuses to render for anybody else. */
/* Picking a shift's tint, in the Settings row.

   Presets first, the free picker last, on its own bordered row. Both are offered
   because the free picker is safe here -- tintStyle turns any hue into a wash that
   carries the cell's ordinary text -- but six unconstrained tints on one printed
   sheet is a legend nobody can order, which is why the six come first and the
   hundred come after. A control that offers a hundred choices before offering the
   six right ones is one most managers never finish. */

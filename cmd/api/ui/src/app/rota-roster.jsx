function ShiftTintButton({ t, shift, onPick }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  const panelRef = useRef(null);

  // The SAME latent bug this session fixed for the cell picker, hit before anybody
  // reported it: this panel opened downward inside .set-group-inner{overflow:hidden}
  // -> .content -> .main -> .app, with openUp never passed a value, so a shift at the
  // bottom of its Settings group was clipped. Fixed here for free by the panel becoming
  // position:fixed, plus the same hook so it flips when there is no room below.
  useAnchoredPanel(open, wrapRef, panelRef, () => setOpen(false));

  // Verbatim from RotaPicker, and deliberately so: one set of answers to "what does
  // Escape do here" rather than two that can drift.
  useEffect(() => {
    if (!open) return;
    const away = e => { if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false); };
    const esc = e => {
      if (e.key !== 'Escape') return;
      e.claimedByDialog = true;
      setOpen(false);
    };
    document.addEventListener('mousedown', away);
    document.addEventListener('keydown', esc, true);
    return () => {
      document.removeEventListener('mousedown', away);
      document.removeEventListener('keydown', esc, true);
    };
  }, [open]);

  const name = shiftTintName(t, shift.color || '');

  return (
    <div className="rota-tint-wrap" ref={wrapRef}>
      <button type="button"
        className={'rota-tint-btn' + (shift.color ? ' is-set' : '')}
        style={tintStyle(shift)}
        aria-expanded={open}
        aria-haspopup="true"
        aria-label={t.shiftTint + ' \u2014 ' + shift.name + ': ' + name}
        title={name}
        onClick={() => setOpen(o => !o)}>
        <span className="rota-tint-btn-ic" aria-hidden="true">
          {shift.color ? Ic.check : Ic.palette}
        </span>
      </button>

      {open && (
        <div className="rota-picker-panel rota-tint-pop" ref={panelRef}
          role="group" aria-label={t.shiftTintPicker.replace('{name}', shift.name)}>
          {/* Real radios, not role=radio on a div. A native radio group brings
              arrow-key traversal, the checked state and the tab stop for free, and
              this file's one existing segmented control cannot help here because it
              has two options and no third state. */}
          <div className="rota-tint-grid" role="radiogroup"
            aria-label={t.shiftTintPicker.replace('{name}', shift.name)}>
            {SHIFT_TINTS.map(c => (
              <label key={c || 'none'}
                className={'rota-tint' + (!c ? ' is-none' : '') + ((shift.color || '') === c ? ' is-on' : '')}
                style={tintStyle({ color: c })}
                title={shiftTintName(t, c)}>
                <input type="radio" name={'shift-tint-' + shift.id} value={c}
                  checked={(shift.color || '') === c}
                  onChange={() => onPick(c)}/>
                <span className="rota-tint-ic" aria-hidden="true">
                  {c ? Ic.check : Ic.diag}
                </span>
                {/* The word is in the DOM, not only in the title: at 26px a 10px
                    label under each tile is legible, and it is the same redundant cue
                    every grid cell gets from the shift name. */}
                <span className="rota-tint-cap">{shiftTintName(t, c)}</span>
              </label>
            ))}
          </div>

          <div className="rota-tint-custom">
            <input type="color" className="color-swatch-input"
              value={shift.color || '#5b6779'}
              aria-label={t.shiftTintCustom + ' \u2014 ' + shift.name}
              onChange={e => onPick(e.target.value)}/>
            <span>{t.shiftTintCustom}</span>
          </div>
        </div>
      )}
    </div>
  );
}

/* Who is on the team rota. In Settings, not on the rota page.

   That location is not incidental. The rota page is PUBLIC -- it is a noticeboard -- and
   the only gate it has is can_edit. Any control near the "6 people, 4 shifts" summary
   would either be rendered for everybody or appear only once a session resolves, in
   which case the grid re-flows under the manager's cursor as they sign in. Worse,
   membership changes CHANGE the grid, so a control that mutates the thing immediately
   below it -- on a page anonymous visitors are reading at the same time -- invites the
   "did I just delete Marco?" panic.

   Settings already holds the reasoning: "the shift templates are in Settings rather than
   on the rota page because they are configuration, edited rarely, by somebody who knows
   the hotel's hours, while the rota itself is edited often". Rota membership is MORE
   configuration than shift templates -- edited a few times a year, blast radius the whole
   team. So it belongs beside them, in a group of its own, reached from the rota page by
   one quiet manager-only link.

   `picked` below is deliberately NOT the rota's cell draft and lives in a different
   component entirely. dirtyIds diffs the draft against data.members by days[i].id, so a
   membership change sharing that state would read as cell edits and the rota's save bar
   would count people who edited nothing. */

/* The roster's ids in the SERVER's order.
   Module level rather than a local, because three places need it -- load, reset and the
   move count -- and each of them re-deriving it is each of them getting a chance to
   disagree about what "the order" means. */
function onRotaIDs(rows) {
  return (rows || []).filter(m => m.on_rota).map(m => m.user_id);
}

/* One row of the picker.
   label > input, visually hidden, tick drawn on a span -- the same shape as
   .rota-abs-kind and .rota-tint, so the file now has three multi-select-style controls
   and one implementation of it.

   The label is NOT the row. It used to be, and that is exactly why reordering could not
   live here: a button inside a label is inside the label's activation behaviour, so
   pressing Space or Enter on a "move up" arrow would fire the label's default action
   and TOGGLE MEMBERSHIP. The same goes for the click the browser synthesises from a
   pointerdown-and-release on the arrow.

   So the label is now ONE of the row's three children, alongside the rank, the grip and
   the arrows, and it still wraps everything it used to. That keeps the property the
   original comment below describes -- the badges and the kept-shifts count reach the
   accessible name for free -- and removes the hazard at its source rather than papering
   over it with stopPropagation on every handler. */
function RosterRow({ t, m, checked, onToggle, showCells,
                      orderable, index, last, isRoving,
                      dragging, flashed, dropBefore, dropAfter,
                      onMove, onMoveKey, onGripDown, onGripMove, onGripUp,
                      btnRef, gripRef }) {
  const cls = 'roster-row'
    + (m.active ? '' : ' is-inactive')
    // is-unset marks "no manager has an opinion about where this person goes". It is a
    // styling hook only -- the visual difference is the dash, which is always drawn for
    // an unset row whether or not a rule uses this.
    + (orderable && !m.rota_position ? ' is-unset' : '')
    // Background and opacity only. No transform anywhere in these three states, so
    // nothing reflows and nothing animates -- which is why the ordering CSS block has
    // no prefers-reduced-motion rule to reduce.
    + (dragging ? ' is-dragging' : '')
    + (flashed ? ' is-moved' : '')
    + (dropBefore ? ' drop-before' : '')
    + (dropAfter ? ' drop-after' : '');

  return (
    <div className={cls}
      /* Hit-testing during a drag needs the row's own identity, which is the same data
         attribute the rota grid puts on its cells. */
      data-rota-u={orderable ? m.user_id : undefined}>
      {orderable && (
        /* The DISPLAY index, never the stored position. rota_position is an
           implementation detail: it can be null, it can be stale, and it can collide
           after somebody is excluded and re-added -- so printing it would show a number
           the grid is not actually honouring.

           aria-hidden because the numeral's job is a positional cue for a sighted
           reader. The accessible name is the label's, the reorder buttons name their
           target explicitly, and the live region announces the RESULT of a move. */
        <span className="roster-rank" aria-hidden="true">{m.rota_position ? index + 1 : '—'}</span>
      )}

      {orderable && (
        /* POINTER ONLY. tabIndex -1 keeps it out of the tab order; the title documents
           the affordance for the mouse user who can see it, and the arrow buttons carry
           every name a screen reader needs. The whole point of the arrows is that they
           are the complete interface and this is an extra. */
        <button type="button" className="roster-grip"
          tabIndex={-1}
          ref={gripRef}
          onPointerDown={onGripDown}
          onPointerMove={onGripMove}
          onPointerUp={onGripUp}
          onPointerCancel={onGripUp}
          title={t.rosterOrderGrip.replace('{name}', shortName(m.name))}>
          {Ic.brackets}
        </button>
      )}

      <label className="roster-pick">
        <input type="checkbox" className="roster-box"
          checked={!!checked}
          onChange={() => onToggle(m.user_id)}/>
        <span className="roster-tick" aria-hidden="true">{checked ? Ic.check : null}</span>

        <Avatar name={shortName(m.name)} idx={m.avatar_idx} size="sm"/>

        <span className="roster-id">
          <span className="roster-name">{shortName(m.name)}</span>
          {/* Role is a fact about the ACCOUNT, not about rota membership, so it is a
              badge and never a reason to be excluded. */}
          {m.role === 'manager' && <span className="roster-role">{t.rosterRoleManager}</span>}
          {/* Deactivated is a different concept from excluded and gets its own badge.
              t.inactive is reused verbatim so one concept has one name across the app --
              it is the same string the Team page shows. */}
          {m.active === false && <span className="roster-inactive">{t.inactive}</span>}
        </span>

        {/* The "nothing is lost" sentence, and the only place it can live.

            Shown in the OFF-rota section only. On the rota it is redundant -- an included
            person with cells is just a working rota, which the grid already shows -- while
            off the rota it is the entire difference between a reversible board change and
            something that reads as erasure. That asymmetry is also why the picker needs
            two sections rather than one.

            Grey, reusing .rota-free, because nothing here is an alarm. A row of "you are
            about to lose 12 shifts" would be arithmetically true, operationally
            irrelevant and emotionally decisive in exactly the wrong direction. */}
        {showCells && m.assigned_cells > 0 && (
          <span className="rota-free is-away">
            {Ic.slash}
            {t.rosterKeepsCells.replace('{count}', m.assigned_cells)}
          </span>
        )}
      </label>

      {orderable && (
        /* ROVING TABINDEX: two tab stops for the entire control set, whatever the team
           size. Without it twenty people costs forty tab stops.

           Rows are keyed by user_id, not by index, so React MOVES the DOM node on a
           reorder instead of recreating it and focus survives the move for free. An
           index key would remount the button and drop focus to <body> on every press --
           which is why the list below keys on m.user_id and must keep doing so. */
        <span className="roster-moves">
          <button type="button"
            className="roster-move icon-btn is-up"
            tabIndex={isRoving ? 0 : -1}
            ref={el => btnRef('up', el)}
            disabled={index === 0}
            onKeyDown={e => onMoveKey(e, 'up')}
            onClick={() => onMove(-1)}
            title={t.rosterOrderUp}
            aria-label={t.rosterOrderUpOf.replace('{name}', shortName(m.name))}>
            {Ic.chevron}
          </button>
          <button type="button"
            className="roster-move icon-btn is-down"
            tabIndex={isRoving ? 0 : -1}
            ref={el => btnRef('down', el)}
            disabled={index === last}
            onKeyDown={e => onMoveKey(e, 'down')}
            onClick={() => onMove(+1)}
            title={t.rosterOrderDown}
            aria-label={t.rosterOrderDownOf.replace('{name}', shortName(m.name))}>
            {Ic.chevron}
          </button>
        </span>
      )}
    </div>
  );
}

/* The picker. One Save writes the whole set; there is no per-toggle write.

   Immediate per-toggle writes would be five PUTs for five boxes, a mid-sequence failure
   that leaves the roster half-applied with no record of what was intended, and no place
   to summarise what is about to happen -- which is the whole answer to "tick five boxes
   and close the tab". The backend takes a whole set precisely so this can be one
   deliberate act. */
function RosterCard({ token, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];

  const [data, setData] = useState(null);
  // A Set of user_ids, NOT a draft, and deliberately not shaped like one. See the note
  // above RosterRow about dirtyIds.
  const [picked, setPicked] = useState(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  // The ORDER draft, and it is a different thing from `picked` on purpose.
  //
  // An ARRAY of user_ids, because this is an ORDER and position is the data. A Set would
  // throw away exactly the thing the control edits.
  //
  // It is NEVER fed into `picked`, and that is load-bearing rather than tidiness. The
  // roster PUT is order-insensitive -- Set writes `user_id = ANY($1)` -- so leaking the
  // permuted sequence into member_ids would be silently harmless today and silently
  // wrong the day somebody adds validation to the roster write. `picked` is iterated as
  // `[...picked]` to build member_ids, and that insertion order is the SERVER's order,
  // set once at load and only ever mutated by add/delete.
  const [order, setOrder] = useState(null);
  // A separate error strip from `err`, because the two requests can half-succeed and a
  // single strip would have to lie about one of them. See the catch block in write().
  const [orderErr, setOrderErr] = useState('');
  const [announce, setAnnounce] = useState('');
  const [confirmOff, setConfirmOff] = useState(null);
  const [confirmEmpty, setConfirmEmpty] = useState(false);

  // Which row's arrows are in the tab order. A user_id, not an index, so it survives a
  // reorder -- an index would point at a different person after the row it named moved.
  const [roving, setRoving] = useState(null);
  // The row under the pointer mid-drag, and which half of it. Null when no drag.
  const [drag, setDrag] = useState(null);
  // The row that just moved, flashed and then cleared. A timer rather than a
  // transition, so there is no motion to reduce -- see the ordering CSS block.
  const [flash, setFlash] = useState(null);

  // Refs for the reorder buttons, keyed by user_id and direction, so Shift+Arrow and
  // Home/End can move DOM FOCUS and not merely the roving index. Focus does not follow
  // a tabIndex change on its own: the element stays focused and simply becomes -1, which
  // leaves the keyboard user stranded on the row they just left.
  const btnRefs = useRef({});
  const gripRefs = useRef({});
  // The drag lives in a REF, mirrored into state only for rendering.
  //
  // Reading the drop target out of state would be a bug: a pointerup can arrive before
  // React has flushed the last setDrag, so the target would be one move stale and the
  // row would land somewhere the pointer was rather than somewhere it is. A ref is
  // written synchronously and read synchronously, which is the only ordering guarantee
  // a pointer sequence gives us.
  const dragRef = useRef(null);
  const flashTimer = useRef(null);

  // Rebuild `picked` from the PAYLOAD on every load, never from what this client last
  // sent. The roster's truth is the server's answer, and a rejected write must not be
  // able to leave a checkbox disagreeing with it.
  const load = useCallback(() => apiFetch('/rota/members', { token })
    .then(res => {
      const rows = res.members || [];
      setData({ members: rows, canEdit: !!res.can_edit });
      setPicked(new Set(rows.filter(m => m.on_rota).map(m => m.user_id)));
      // The order draft starts as the SERVER's order, so "moved" is false on load and
      // stays false until somebody actually reorders.
      setOrder(onRotaIDs(rows));
      setErr('');
      setOrderErr('');
      setAnnounce('');
      const n = rows.filter(m => m.on_rota).length;
      setTimeout(() => setAnnounce(t.rosterLoaded.replace('{count}', n)), 30);
    })
    .catch(ex => setErr((ex && ex.message) || 'Request failed')),
    [token, t]);

  useEffect(() => { load(); }, [load]);

  // The server orders included-first, so the two sections are a contiguous slice rather
  // than a sort the client has to reproduce and could get out of step with.
  const onRota  = (data && data.members || []).filter(m =>  m.on_rota);
  const offRota = (data && data.members || []).filter(m => !m.on_rota);

  // The on-rota section IN DRAFT ORDER. Sorted by the draft's index rather than by the
  // payload's position, with unranked rows last so a person added since load cannot
  // disappear off the end of the list.
  const rank = {};
  (order || []).forEach((id, i) => { rank[id] = i; });
  const onRotaOrdered = onRota.slice().sort((a, b) => {
    const ra = rank[a.user_id] === undefined ? 1e9 : rank[a.user_id];
    const rb = rank[b.user_id] === undefined ? 1e9 : rank[b.user_id];
    return ra - rb;
  });

  // How many people's membership differs from what is stored. Added and removed together,
  // matching the "N people, M shifts" grammar the rota subtitle already uses.
  const changed = (picked && data)
    ? data.members.filter(m => picked.has(m.user_id) !== !!m.on_rota).length
    : 0;

  // How many PEOPLE changed PLACE, which is a different count from `changed` and a
  // different fact. Counting positions rather than rows is deliberate: [A,B,C] -> [B,A,C]
  // moves two people, and the sentence should say so.
  const moved = (order && data)
    ? order.reduce((n, id, i) => {
        const saved = onRotaIDs(data.members);
        return saved[i] !== id ? n + 1 : n;
      }, 0)
    : 0;

  // Whether the dash needs explaining. True while ANY on-rota person is unplaced, and
  // false the moment the first order is saved, because that write gives every listed
  // person a position. So the note has exactly one lifetime: from "somebody has never
  // been moved" to "everybody has".
  const hasUnplaced = onRota.some(m => !m.rota_position);

  // Who is about to LOSE cells: currently on the rota, has some, and now unticked.
  // Computed from `picked` rather than after the fact, because it is what the
  // confirmation has to list.
  const losing = (picked && data)
    ? data.members.filter(m => m.on_rota && m.assigned_cells > 0 && !picked.has(m.user_id))
    : [];

  const toggle = id => setPicked(s => {
    const next = new Set(s);
    next.has(id) ? next.delete(id) : next.add(id);
    return next;
  });

  // Cancel puts BOTH drafts back. One button, one meaning: "undo what I did here", and
  // a button that undid only the boxes while leaving a rearranged list on screen would
  // be two meanings wearing one label.
  function reset() {
    setPicked(new Set(data.members.filter(m => m.on_rota).map(m => m.user_id)));
    setOrder(onRotaIDs(data.members));
    setOrderErr('');
  }

  // -- The ordering controls ------------------------------------------------------

  /* SHORTENED, unlike SchedulePage's identically-named nameFor, because this is a
     Settings screen and Settings is not the rota. The two helpers are the same
     shape on purpose -- both read the same members array -- so they are easy to
     confuse, and the difference is a deliberate exception rather than an
     oversight: the rota keeps whole names, everything else does not. */
  const nameOf = id => {
    const m = (data && data.members || []).find(x => x.user_id === id);
    return m ? shortName(m.name) : '';
  };

  // The repeated-string trick: an identical message set twice in a row is announced
  // once, so a 30ms delay is what makes "already first" speak every time it is pressed.
  function say(msg) { setTimeout(() => setAnnounce(msg), 30); }

  function flashRow(id) {
    setFlash(id);
    if (flashTimer.current) clearTimeout(flashTimer.current);
    flashTimer.current = setTimeout(() => setFlash(null), 600);
  }

  function commitMove(next, movedId, place) {
    setOrder(next);
    setRoving(movedId);
    flashRow(movedId);
    // Who, where, out of how many. Fewer than all three and it is not an announcement.
    say(t.rosterOrderMoved
      .replace('{name}', nameOf(movedId))
      .replace('{place}', String(place + 1))
      .replace('{total}', String(next.length)));
  }

  // Move one place. dir is -1 or +1.
  function moveBy(id, dir) {
    if (!order) return;
    const i = order.indexOf(id);
    if (i < 0) return;
    const j = i + dir;
    // Saying so beats silently doing nothing: the button is DIMMED rather than removed,
    // so it is still there to be pressed, and a press that does nothing needs a reason.
    if (j < 0) { say(t.rosterOrderFirst.replace('{name}', nameOf(id))); return; }
    if (j >= order.length) { say(t.rosterOrderLast.replace('{name}', nameOf(id))); return; }
    const next = order.slice();
    next.splice(j, 0, next.splice(i, 1)[0]);
    commitMove(next, id, j);
  }

  /* THE KEYBOARD MODEL.

       Enter / Space   the button's own activation -> move one place
       ArrowUp/Down    MOVE this person one place
       Shift+Arrow     move FOCUS to the neighbouring row, no reorder
       Home / End      focus the first / last row
       Escape          put the order draft back as it was

     The shift modifier is the whole collision story. Bare arrows are left FREE and the
     modifier is what means "I'm navigating" -- which is the discipline onDayKeyDown uses
     for the rota grid, and the standard modified-vs-unmodified idiom, so it is not a
     house invention. Every arrow is preventDefault'd, because without it the page
     scrolls on every reorder: on a twenty-row list that is nineteen page-scrolls.

     Every path -- mouse, touch, keyboard, switch access -- goes through moveBy. Drag is
     an EXTRA, not an alternative, and the arrow buttons are the complete interface. */
  function orderKey(e, id, dir) {
    if (!order) return;
    const k = e.key;
    const i = order.indexOf(id);
    if (i < 0) return;

    if (k === 'ArrowUp' || k === 'ArrowDown') {
      e.preventDefault();
      if (e.shiftKey) {
        const ni = k === 'ArrowUp' ? i - 1 : i + 1;
        if (ni >= 0 && ni < order.length) {
          setRoving(order[ni]);
          // Focus the SAME arrow direction in the row we landed on, so the control the
          // key is acting on stays under the same finger. tabIndex -1 elements are
          // focusable programmatically, so this works before the re-render too.
          const el = btnRefs.current[order[ni] + dir];
          if (el) el.focus();
        }
      } else {
        moveBy(id, k === 'ArrowUp' ? -1 : +1);
      }
      return;
    }
    if (k === 'Home' || k === 'End') {
      e.preventDefault();
      const target = k === 'Home' ? order[0] : order[order.length - 1];
      setRoving(target);
      const el = btnRefs.current[target + dir];
      if (el) el.focus();
      return;
    }
    if (k === 'Escape') {
      e.preventDefault();
      reset();
      say(t.rosterOrderReset);
    }
  }

  /* THE DRAG. Pointer-only, off the grip, and additive.

     It starts on the GRIP rather than on the row, because a row-wide pointerdown would
     turn every text selection in a name into a reorder attempt. It lives entirely on the
     grip's pointer events with a pointer capture, so a drag that leaves the row still
     reports, and the rows are hit-tested with elementFromPoint + closest -- the same
     data-attribute approach the rota grid uses for its own cells. */
  function gripDown(e, id) {
    if (!canEditNow() || !order) return;
    e.preventDefault();
    dragRef.current = { id, at: null };
    setDrag({ from: id, at: null });
    setRoving(id);
    // Capture on THE GRIP THAT WAS PRESSED, so a drag that wanders off the row keeps
    // reporting. Capturing on some other element -- or not at all -- means a pointerup
    // outside the row is never seen and the card is left stuck mid-drag.
    const el = gripRefs.current[id];
    if (el && el.setPointerCapture) {
      try { el.setPointerCapture(e.pointerId); } catch (ex) { /* capture is an optimisation, not a requirement */ }
    }
  }

  function gripMove(e) {
    const held = dragRef.current;
    if (!held) return;
    const at = dropTarget(e);
    held.at = at;
    setDrag({ from: held.id, at });
  }

  function gripUp() {
    const held = dragRef.current;
    dragRef.current = null;
    setDrag(null);
    // A drop on the row's own half it is already in is a no-op, and applyDrop says so
    // by comparing the result. Releasing with no target -- off the list, or after a
    // pointercancel -- is simply nothing.
    if (held && held.at && held.at.id !== held.id) applyDrop(held.id, held.at);
  }

  // Which row is the pointer over, and which half of it. The vertical midpoint is the
  // test because the rows are a single column and a horizontal one has no answer.
  function dropTarget(e) {
    const el = (typeof document.elementFromPoint === 'function')
      ? document.elementFromPoint(e.clientX, e.clientY) : null;
    const row = el && el.closest ? el.closest('.roster-row[data-rota-u]') : null;
    if (!row || !row.getAttribute('data-rota-u')) return null;
    const r = row.getBoundingClientRect();
    return { id: Number(row.getAttribute('data-rota-u')), before: e.clientY < r.top + r.height / 2 };
  }

  function applyDrop(fromId, target) {
    const rest = order.filter(x => x !== fromId);
    let j = rest.indexOf(target.id);
    if (j < 0) return;
    if (!target.before) j += 1;
    const next = rest.slice();
    next.splice(j, 0, fromId);
    if (next.join() === order.join()) return;
    commitMove(next, fromId, j);
  }

  // Dropped mid-drag on its own row, or on a neighbour with no actual displacement.
  const drop = (drag && drag.at) || null;
  const dragging = (drag && drag.from) || null;

  // Which row's arrows are the two tab stops. Null until load, and falling back to the
  // first row so the set is always reachable.
  const rovingId = (roving && order && order.indexOf(roving) !== -1)
    ? roving : (order && order[0] !== undefined ? order[0] : null);

  // -- The writes ------------------------------------------------------------------

  const canEditNow = () => !!(data && data.canEdit);

  /* Sends ONE order and returns the refreshed rows. It throws rather than setting state,
     because two callers need different reactions to a failure and neither of them is
     "report it and carry on". */
  async function sendOrder(ids) {
    const res = await apiFetch('/rota/members/order', {
      method: 'PUT', token, body: { user_ids: ids },
    });
    return res.members || [];
  }

  /* Reconciles the draft to a membership.

     Keeps the manager's order for whoever survived, drops whoever left, and appends
     newcomers in the SERVER's order at the end. Called after the roster write, because
     that write can add or remove people the draft knows nothing about -- and skipping
     this would leave an id in the draft that is no longer on the rota, which would then
     be sent as an order entry for somebody who is not on the board. */
  function reconcile(prev, ids) {
    const keep = (prev || []).filter(id => ids.indexOf(id) !== -1);
    const added = ids.filter(id => keep.indexOf(id) === -1);
    return keep.concat(added);
  }

  // Both writes go through here, so the confirm-then-send path and the straight path
  // cannot drift apart. `confirmEmpty` is the server's acknowledge-the-empty-case flag,
  // and it is only ever sent from the dialog that asked the question -- asking at the
  // client keeps the reason next to the button that caused it rather than in a toast
  // afterwards.
  async function write(ids, confirmedEmpty) {
    const pending = moved > 0 ? reconcile(order, ids) : null;
    setBusy(true);
    setErr('');
    setOrderErr('');
    try {
      const res = await apiFetch('/rota/members', {
        method: 'PUT', token,
        body: { member_ids: ids, confirm_empty: !!confirmedEmpty },
      });
      // THE RESPONSE IS THE TRUTH. Not an echo of what was sent and not an optimistic
      // patch: for most accounts the answer is decided by a row this request never
      // touched, so the client could not derive it correctly even if it wanted to.
      const rows = res.members || [];
      setData({ members: rows, canEdit: !!res.can_edit });
      setPicked(new Set(rows.filter(m => m.on_rota).map(m => m.user_id)));
      setConfirmOff(null);
      setConfirmEmpty(false);
      setOrder(reconcile(order, onRotaIDs(rows)));

      if (!pending) {
        say(t.rosterSaved);
        if (setToast) setToast(t.rosterSaved);
        return;
      }

      // The roster was accepted, so the order draft was computed against a board that
      // now exists and it is safe to seat people on it. Sent second and never first: the
      // draft names people the refused roster may have just removed.
      try {
        const rows2 = await sendOrder(pending);
        setData({ members: rows2, canEdit: !!res.can_edit });
        setOrder(onRotaIDs(rows2));
        say(t.rosterOrderSaved);
        if (setToast) setToast(t.rosterSaved);
      } catch (ex2) {
        /* HALF SUCCESS, and this is the case that gets told wrong most often.

           The membership genuinely saved. Saying "failed" would be a lie; saying
           "saved" with nothing on screen would be a different lie. So the toast reports
           the half that worked, the strip below the list reports the half that did not,
           and the order draft STAYS DIRTY so the retry is one click rather than a
           reconstruction. */
        setOrderErr((ex2 && ex2.message) || t.rosterOrderFailed);
        say(t.rosterSaved);
        if (setToast) setToast(t.rosterSaved);
      }
    } catch (ex) {
      setErr((ex && ex.message) || 'Request failed');
      // BOTH drafts stay dirty and NEITHER is cleared. The order was computed against a
      // roster the server just refused, so sending it could seat somebody who is about
      // to be taken off the board.
    } finally {
      setBusy(false);
    }
  }

  // Order-only save: nothing ticked changed, so the roster PUT is skipped entirely.
  // Two requests where one is warranted would be two failure modes for one intent.
  async function writeOrderOnly() {
    setBusy(true);
    setOrderErr('');
    try {
      const rows = await sendOrder(reconcile(order, onRotaIDs(data.members)));
      setData({ members: rows, canEdit: !!data.canEdit });
      setOrder(onRotaIDs(rows));
      say(t.rosterOrderSaved);
      if (setToast) setToast(t.rosterOrderSaved);
    } catch (ex) {
      setOrderErr((ex && ex.message) || t.rosterOrderFailed);
    } finally {
      setBusy(false);
    }
  }

  function save() {
    if (changed === 0) { if (moved > 0) writeOrderOnly(); return; }
    const ids = [...picked];
    // The empty case gets its own short dialog rather than a longer version of the same
    // one: it is a different decision, not a bigger version of it.
    if (ids.length === 0) { setConfirmEmpty(true); return; }
    // Confirm BEFORE writing, not after. A confirmation that arrives once the change has
    // already happened cannot prevent it.
    if (losing.length > 0) { setConfirmOff({ ids, people: losing }); return; }
    write(ids, false);
  }

  if (err && !data) return <div className="auth-error">{err}</div>;
  if (!data) return <p className="set-empty">{t.loading}</p>;

  const canEdit = data.canEdit;

  return (
    <div className="roster">
      {/* One polite live region for the whole picker. Set on load and on save, and --
          as one documented exception -- on every MOVE.

          Rows swapping places is invisible to a screen reader: nothing in the DOM says
          "Ana is now above Marco", the ordinal is aria-hidden, and a reorder is not a
          native control that speaks for itself. So this is the one event that must
          speak. Ticking a box still does not, because the checkbox is native and already
          announces itself, and the save bar's counts are visible to everybody else. */}
      <div className="sr-only" role="status" aria-live="polite">{announce}</div>

      {/* The sentence that keeps two orthogonal concepts apart, said once and here
          rather than per row. Deactivating changes the ACCOUNT; unticking changes the
          BOARD. Both make a row change, which is exactly why a manager needs to be told
          which is which -- and why this picker never calls the deactivate endpoint, and
          the Team page never calls the roster one. */}
      {canEdit && (
        <div className="rota-note">
          {Ic.info}
          <span>{t.rosterExcludeNote}</span>
        </div>
      )}

      {/* The same idea about the DASH, and only while a dash exists. Once every row
          carries a number there is nothing here to explain, and a note explaining a
          thing that is no longer on screen is noise that outlives its cause. */}
      {canEdit && hasUnplaced && (
        <div className="rota-note">
          {Ic.info}
          <span>{t.rosterOrderNote}</span>
        </div>
      )}

      {canEdit && (
        <>
          <div className="roster-section">
            {t.rosterOnRotaHead}
            <span className="roster-count">{onRota.length}</span>
          </div>
          <div className="roster-list">
            {onRota.length === 0
              ? <p className="set-empty">{t.rosterNoneOn}</p>
              : onRotaOrdered.map((m, i) => (
                  <RosterRow key={m.user_id} t={t} m={m}
                    checked={picked.has(m.user_id)} onToggle={toggle} showCells={false}
                    orderable index={i} last={onRotaOrdered.length - 1}
                    isRoving={rovingId === m.user_id}
                    dragging={dragging === m.user_id}
                    flashed={flash === m.user_id}
                    dropBefore={!!drop && drop.id === m.user_id && drop.before}
                    dropAfter={!!drop && drop.id === m.user_id && !drop.before}
                    onMove={d => moveBy(m.user_id, d)}
                    onMoveKey={(e, dir) => orderKey(e, m.user_id, dir)}
                    btnRef={(dir, el) => {
                      const k = m.user_id + dir;
                      if (el) btnRefs.current[k] = el; else delete btnRefs.current[k];
                    }}
                    gripRef={el => {
                      if (el) gripRefs.current[m.user_id] = el;
                      else delete gripRefs.current[m.user_id];
                    }}
                    onGripDown={e => gripDown(e, m.user_id)}
                    onGripMove={gripMove}
                    onGripUp={gripUp}/>
                ))}
          </div>

          <div className="roster-section">
            {t.rosterOffRotaHead}
            <span className="roster-count">{offRota.length}</span>
          </div>
          <div className="roster-list">
            {offRota.length === 0
              ? <p className="set-empty">{t.rosterEveryoneOn}</p>
              : offRota.map(m => (
                  <RosterRow key={m.user_id} t={t} m={m}
                    checked={picked.has(m.user_id)} onToggle={toggle} showCells/>
                ))}
          </div>
        </>
      )}

      {!canEdit && (
        <p className="set-empty">
          {t.rosterReadOnly}
        </p>
      )}

      {err && <div className="auth-error">{err}</div>}

      {/* The order's OWN failure, directly above the save bar so it sits next to the thing
          that failed. It is a separate strip from the membership error because the two
          requests can half-succeed, and one strip would have to lie about one of them. */}
      {orderErr && <div className="auth-error">{orderErr}</div>}

      {/* Reusing .rota-savebar verbatim rather than inventing a second one. It sits
          after the list in the DOM, so Tab out of the last row lands on Cancel and then
          Save with no key handling at all -- the same reason the rota's own save bar
          lives there, and its comment says so.

          ONE bar for BOTH drafts, not two. A manager adding two people in a particular
          place does it in one visit; two bars force two clicks for one intent and put two
          Cancel buttons on screen that nobody can tell apart. The bar renders on either
          draft being dirty, and its sentence enumerates both. */}
      {canEdit && (changed > 0 || moved > 0) && (
        <div className="rota-savebar">
          {Ic.info}
          <span className="roster-dirty">
            {changed > 0 && (
              <span>{changed === 1
                ? t.rosterUnsavedOne : t.rosterUnsaved.replace('{count}', changed)}</span>
            )}
            {moved > 0 && (
              <span>{moved === 1
                ? t.rosterOrderUnsavedOne : t.rosterOrderUnsaved.replace('{count}', moved)}</span>
            )}
          </span>
          <span className="rota-savebar-spacer" />
          <button type="button" className="btn btn-ghost" onClick={reset} disabled={busy}>
            {t.rotaCancel}
          </button>
          <button type="button" className="btn btn-primary" onClick={save}
            disabled={busy}>
            {Ic.check} {busy ? '.' : t.rosterSave}
          </button>
        </div>
      )}

      {confirmOff && <ConfirmRosterOff
        t={t}
        people={confirmOff.people}
        busy={busy}
        onCancel={() => setConfirmOff(null)}
        onConfirm={() => write(confirmOff.ids, false)}
      />}

      {confirmEmpty && <ConfirmEmptyRoster
        t={t}
        busy={busy}
        onCancel={() => setConfirmEmpty(false)}
        onConfirm={() => write([], true)}
      />}
    </div>
  );
}

/* Taking people off the rota.

   The load-bearing part is the body copy, and it says the shifts COME BACK. The same
   sentence exists in rotaAbsRemoveBody for absences, for the same reason: a manager who
   believes the work is deleted will untick nobody, forever. The list names every affected
   person rather than counting them, because "3 people" is not something anybody can check
   against the five boxes they just ticked.

   btn-danger even though nothing is deleted, because DiscardDialog sets the precedent
   and the reason holds: the button is the consequential one, and the colour is a promise
   that says so.

   The SAFE action takes the initial focus. A reflexive Enter after mis-clicking a
   checkbox must not take three people off the rota -- the same reasoning as DiscardDialog
   and ConfirmDeleteIssue, and the reason those two pass initialFocus explicitly rather
   than letting the header's X have it. */
function ConfirmRosterOff({ t, people, onCancel, onConfirm, busy }) {
  const panelRef = useDialog({ onClose: busy ? null : onCancel, initialFocus: '#roster-off-cancel' });
  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && !busy && onCancel()}>
      <div className="modal confirm-modal" ref={panelRef} role="dialog" aria-modal="true"
        aria-labelledby="roster-off-title">
        <div className="modal-header">
          <h2 id="roster-off-title">
            {people.length === 1
              ? t.rosterOffTitleOne
              : t.rosterOffTitle.replace('{count}', people.length)}
          </h2>
          <button type="button" className="icon-btn" onClick={onCancel} disabled={busy}>{Ic.x}</button>
        </div>

        <div className="modal-body">
          <p className="confirm-text">{t.rosterOffBody}</p>
          <ul className="roster-offlist">
            {people.map(p => (
              <li key={p.user_id} className="roster-offlist-row">
                <Avatar name={shortName(p.name)} idx={p.avatar_idx} size="sm"/>
                <span className="roster-offlist-name">{shortName(p.name)}</span>
                {p.assigned_cells > 0 && (
                  <span className="rota-free is-away">
                    {Ic.slash}
                    {t.rosterKeepsCells.replace('{count}', p.assigned_cells)}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </div>

        <div className="modal-footer">
          <button type="button" id="roster-off-cancel" className="btn btn-ghost"
            onClick={onCancel} disabled={busy} autoFocus>{t.rotaCancel}</button>
          <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={busy}>
            {busy ? t.deleting : <>{Ic.check} {t.rosterOffYes}</>}
          </button>
        </div>
      </div>
    </div>
  );
}

/* The empty roster. Separate because it is a different decision, not a longer version of
   the same one -- and because the API asks for it explicitly with confirm_empty, so the
   acknowledgement has to be made in one identifiable place rather than inferred from a
   dialog that is doing something else. */
function ConfirmEmptyRoster({ t, onCancel, onConfirm, busy }) {
  const panelRef = useDialog({ onClose: busy ? null : onCancel, initialFocus: '#roster-empty-cancel' });
  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && !busy && onCancel()}>
      <div className="modal confirm-modal" ref={panelRef} role="dialog" aria-modal="true"
        aria-labelledby="roster-empty-title">
        <div className="modal-header">
          <h2 id="roster-empty-title">{t.rosterEmptyTitle}</h2>
          <button type="button" className="icon-btn" onClick={onCancel} disabled={busy}>{Ic.x}</button>
        </div>
        <div className="modal-body confirm-body">
          <span className="confirm-icon">{Ic.users}</span>
          {/* No confirm-subject here, unlike the other two confirmations in this file.

             The subject line exists to NAME the thing being acted on -- a shift's hours,
             an absence's dates -- and there is nothing to name here: the subject is
             the rota itself, which the dialog title already says. Reusing
             t.rotaFreeWeek as filler rendered a stray "Not on this rota" inside the
             dialog, which reads like a field label pointing at nothing. */}
          <div className="confirm-text">{t.rosterEmptyBody}</div>
        </div>
        <div className="modal-footer">
          <button type="button" id="roster-empty-cancel" className="btn btn-ghost"
            onClick={onCancel} disabled={busy} autoFocus>{t.rotaCancel}</button>
          <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={busy}>
            {busy ? t.deleting : <>{Ic.check} {t.rosterEmptyYes}</>}
          </button>
        </div>
      </div>
    </div>
  );
}


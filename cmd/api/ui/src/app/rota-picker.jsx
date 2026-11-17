function useAnchoredPanel(open, anchorRef, panelRef, onAnchorGone) {
  const place = React.useCallback(() => {
    const a = anchorRef.current, p = panelRef.current;
    if (!a || !p) return;

    const r = a.getBoundingClientRect();

    // The anchor has left the viewport entirely. Following it would leave a keyboard
    // user holding a listbox they cannot see, so this closes instead. Scrolling the
    // page is fine; losing the thing you opened is not.
    if (r.bottom <= 0 || r.top >= window.innerHeight) {
      if (onAnchorGone) onAnchorGone();
      return;
    }

    const GAP = 4, M = 8;
    const vh = window.innerHeight, vw = window.innerWidth;
    const h = p.getBoundingClientRect().height;

    // Down if it fits below, otherwise up if it fits above, otherwise whichever side
    // has more room -- and that last branch is the only one that can scroll, because
    // the hook writes max-height to whatever room actually exists. At 358px of content
    // on a 890px viewport it is unreachable, which is the point: the requirement is
    // that the panel FITS rather than that it scrolls.
    const down = vh - r.bottom - GAP - M;
    const up   = r.top - GAP - M;
    const below = down >= h || down >= up;
    const room = Math.max(0, below ? down : up);

    p.style.maxHeight = room + 'px';
    p.style.top = (below ? r.bottom + GAP : r.top - GAP - Math.min(h, room)) + 'px';

    // One clamp, replacing the old `i >= 3` column threshold. That number was a
    // measurement of one panel width against one card width at one viewport width; the
    // only edge a fixed panel has to respect is the window's, and this is correct at
    // every width, sidebar width and zoom level.
    const w = p.getBoundingClientRect().width;
    p.style.left = Math.min(Math.max(M, r.left), Math.max(M, vw - M - w)) + 'px';
  }, [anchorRef, panelRef, onAnchorGone]);

  // useLayoutEffect, not useEffect: the panel has to be placed AFTER it has been
  // measured, and this is what guarantees it is painted in the right place before the
  // browser shows it. With a passive effect the first frame puts it at the top-left of
  // the viewport and then snaps it, which is visible.
  React.useLayoutEffect(() => { if (open) place(); }, [open, place]);

  React.useEffect(() => {
    if (!open) return;
    // One frame per scroll burst. There is only ever one open panel, so the cost is one
    // getBoundingClientRect per frame -- cheaper than the alternative of letting the
    // panel drift away from its cell while the grid scrolls under it.
    let raf = 0;
    const tick = () => {
      if (raf) return;
      raf = requestAnimationFrame(() => { raf = 0; place(); });
    };
    window.addEventListener('scroll', tick, true);
    window.addEventListener('resize', tick);
    return () => {
      if (raf) cancelAnimationFrame(raf);
      window.removeEventListener('scroll', tick, true);
      window.removeEventListener('resize', tick);
    };
  }, [open, place]);

  return place;
}

/* One cell's shift chooser.
   The DISMISSAL geometry, the outside-click mousedown and the capture-phase Escape
   handler are RoomPicker's, deliberately. A second popover in this file would mean a
   second set of answers to the questions .room-picker already answers -- what Escape
   does, what a click outside does, whether the panel survives the dialog that owns
   it -- and the two would drift.

   The POSITIONING is not RoomPicker's, and cannot be: it sits in a form with room
   below it, whereas a cell in a 49-cell grid sits under five clipping ancestors, so
   the panel here is position:fixed and placed by useAnchoredPanel above. It used to
   take two props instead -- alignRight={i >= 3} and openUp={false} -- which were a
   measurement of one panel width against one card width at one viewport width, and
   which left the lower rows clipped anyway. */
function RotaPicker({ cellId, value, shifts, absence, onPick, onAddAbsence,
                       onEditAbsence, onRemoveAbsence, memberName, range, selected, selEnd,
                       autoOpen, t }) {
  const [open, setOpen] = useState(false);
  const [cursor, setCursor] = useState(0);
  const wrapRef = useRef(null);
  const panelRef = useRef(null);
  const listId = cellId + '-options';

  // Placement, and only placement. Focus stays on the cell and aria-activedescendant
  // moves -- which is the opposite of what useDialog does, and is why this picker
  // deliberately does not use it.
  useAnchoredPanel(open, wrapRef, panelRef, () => setOpen(false));

  // Opened by a drag rather than by a click. autoOpen is a NONCE, not a boolean:
  // selecting the same cell twice has to open the panel twice, and a boolean that is
  // already true sees no change and does nothing.
  useEffect(() => { if (autoOpen) setOpen(true); }, [autoOpen]);

  useEffect(() => {
    if (!open) return;
    const away = e => { if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false); };
    // Capture phase, and it claims the event -- the same reason RoomPicker does
    // it: Escape should dismiss this panel and leave the page it belongs to
    // alone, and capture reliably runs ahead of a bubble-phase listener on
    // document whatever order they were attached in.
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

  // "Day off" first, then every shift in the order a day happens, then the four
  // absence kinds. ONE flat list, not two: a listbox may only contain options, and a
  // second list would mean a second roving cursor, a second End, and a second set of
  // answers to "where does Down go from here".
  //
  // The current choice is in that list already, so it is picked by clicking it again
  // rather than by a separate control that would have to mean the same thing.
  const shiftOpts = [{off: true, name: t.rotaDayOff, start_time: '', end_time: '', overnight: false}]
    .concat(shifts.map(s => ({ ...s })));
  const options = shiftOpts.concat(ABSENCE_KINDS.map(k => ({ abs: k.id, name: t.rotaAbsKind[k.id] })));
  // Where the absences begin, and therefore where the separator and the second
  // heading go. A named constant rather than an index into `options` at two places,
  // because the positions drift together the moment a shift is added.
  const sepAt = shiftOpts.length;

  const chosenIdx = value
    ? options.findIndex(o => !o.off && o.id === value.id)
    : 0;

  const choose = o => {
    // An absence is not a rota cell, so it does not go through onPick. It opens the
    // dialog and saves itself there -- absence writes are immediate, like a retire in
    // the Settings card, and folding them into a draft that Cancel could throw away
    // would make a holiday something a manager could lose by changing their mind
    // about a shift.
    if (o.abs) { setOpen(false); onAddAbsence && onAddAbsence(o.abs); return; }
    onPick(o.off ? null : o);
    setOpen(false);
  };

  const onKeyDown = e => {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        setOpen(true);
        setCursor(Math.max(chosenIdx, 0));
        e.preventDefault();
      }
      return;
    }
    if (e.key === 'ArrowDown') { setCursor(c => Math.min(c + 1, optionCount - 1)); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { setCursor(c => Math.max(c - 1, 0)); e.preventDefault(); }
    else if (e.key === 'Home') { setCursor(0); e.preventDefault(); }
    else if (e.key === 'End') { setCursor(optionCount - 1); e.preventDefault(); }
    else if (e.key === 'Enter' || e.key === ' ') {
      // Enter picks the highlighted option. With nothing highlighted it is left
      // alone, so the key still does whatever the key does outside a picker.
      // Enter on an absent cell's panel picks whatever is highlighted, which is the
      // three-item list rather than a shift. So the dispatch has to know which panel
      // it is in -- and for this one it opens a dialog instead of writing a cell.
      if (absence) {
        if (cursor === 0) { setOpen(false); }
        else if (cursor === 1) { setOpen(false); onEditAbsence && onEditAbsence(absence); }
        else { setOpen(false); onRemoveAbsence && onRemoveAbsence(absence); }
        e.preventDefault();
        return;
      }
      if (options[cursor] !== undefined) { choose(options[cursor]); e.preventDefault(); }
    }
  };

  // The number of options in the panel that is OPEN, not the shift list's length.
  // An absent cell's panel has three, and Home/End/ArrowDown must address those or a
  // keyboard user can move the cursor onto an option that is not there.
  const optionCount = absence ? 3 : options.length;
  const optionId = i => listId + '-' + i;
  const activeId = (open && options[cursor] !== undefined) ? optionId(cursor) : undefined;

  return (
    <div className="rota-picker" ref={wrapRef}>
      <button type="button"
        className={'rota-cell' + (absence ? ' is-away' : (value ? '' : ' is-off')) +
          (value && value.overnight ? ' is-night' : '') +
          (value && value.color ? ' is-tinted' : '') +
          (selected ? ' is-sel' : '') + (selEnd ? ' is-sel-end' : '') + ' is-editable'}
        /* ONE style attribute answering both features: the absence ink when the day
           is an absence, otherwise the shift's tint. An untinted cell gets a null
           style and keeps its own background, which is what makes it render exactly
           as it did before the tint existed. */
        style={absence ? { '--abs-ink': absenceHue(absence.kind) } : tintStyle(value)}
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={activeId}
        aria-haspopup="listbox"
        onClick={() => { setOpen(o => !o); setCursor(Math.max(chosenIdx, 0)); }}
        onKeyDown={onKeyDown}>
        {absence
          ? <>
              {/* The same four cues as the read-only cell -- hatch, glyph, word --
                  so a cell does not change shape when a manager presses Edit. */}
              <span className="rota-cell-hatch" aria-hidden="true"/>
              <span className="rota-cell-kind" aria-hidden="true">
                {absenceGlyph[absence.kind]}
              </span>
              <span className="rota-cell-absword">{t.rotaAbsShort[absence.kind]}</span>
            </>
          : value
          ? <>
              <span className="rota-cell-name">{value.name}</span>
              {/* "+1" on the name's baseline, so it reads as part of the shift
                  rather than as a badge stuck on the side. */}
              {value.overnight && <span className="rota-cell-plus1">+1</span>}
            </>
          : <span aria-hidden="true">&mdash;</span>}
      </button>

      {open && (
        <div className="rota-picker-panel" ref={panelRef}
          id={listId} role="listbox">
          {/* An absent cell's panel, and it REPLACES the shift list rather than
              sitting under a disabled copy of it. That is where the rule "you cannot
              set a shift on a day somebody is away" is actually enforced: setCell is
              never called, because the options that would call it are not rendered at
              all. Showing a list of shifts that cannot be chosen only invites the
              click. */}
          {absence ? (
            <>
              <div className="rota-abs-locked">{t.rotaCellLabelAway
                .replace('{name}', memberName)
                .replace('{kind}', t.rotaAbsKind[absence.kind])}</div>
              <button type="button" role="option" id={optionId(0)} aria-selected="true"
                className={'rota-option is-abs rota-option-chosen' + (cursor === 0 ? ' active' : '')}
                style={{ '--abs-ink': absenceHue(absence.kind) }}
                onMouseEnter={() => setCursor(0)}
                onMouseDown={e => e.preventDefault()}
                onClick={() => setOpen(false)}>
                <span className="rota-option-tick">{Ic.check}</span>
                <span className="rota-option-kind" aria-hidden="true">
                  {absenceGlyph[absence.kind]}
                </span>
                <span className="rota-option-name">{t.rotaAbsKind[absence.kind]}</span>
                <span className="rota-option-hours">
                  {absence.starts_on.slice(5)}&ndash;{absence.ends_on.slice(5)}
                </span>
              </button>
              <button type="button" role="option" id={optionId(1)} aria-selected="false"
                className={'rota-option' + (cursor === 1 ? ' active' : '')}
                onMouseEnter={() => setCursor(1)}
                onMouseDown={e => e.preventDefault()}
                onClick={() => { setOpen(false); onEditAbsence && onEditAbsence(absence); }}>
                {Ic.edit}<span className="rota-option-name">{t.rotaAbsEditOption}</span>
              </button>
              <button type="button" role="option" id={optionId(2)} aria-selected="false"
                className={'rota-option' + (cursor === 2 ? ' active' : '')}
                onMouseEnter={() => setCursor(2)}
                onMouseDown={e => e.preventDefault()}
                onClick={() => { setOpen(false); onRemoveAbsence && onRemoveAbsence(absence); }}>
                {Ic.trash}<span className="rota-option-name">{t.rotaAbsRemoveOption}</span>
              </button>
              <div className="rota-picker-foot">{t.rotaAbsLocked}</div>
            </>
          ) : options.map((o, i) => {
            const selected = o.off ? !value : (!!value && o.id === value.id);
            return (
              <React.Fragment key={o.abs ? 'abs-' + o.abs : (o.off ? 'off' : o.id)}>
              {/* Both headings, not just a rule. A separator says "there are two
                  lists"; a heading says what the second one IS, and rotaChooseShift --
                  defined in both tables and used nowhere until now -- is exactly the
                  first one's caption. aria-hidden, because the option COUNT a screen
                  reader sees has to stay equal to options.length for End and for
                  aria-activedescendant to mean anything. */}
              {i === 1 && <div className="rota-picker-subhead" role="presentation" aria-hidden="true">{t.rotaChooseShift}</div>}
              {i === sepAt && <>
                <div className="rota-picker-sep" role="presentation" aria-hidden="true"/>
                <div className="rota-picker-subhead" role="presentation" aria-hidden="true">{t.rotaAbsGroupHead}</div>
              </>}
              <button type="button"
                role="option"
                id={optionId(i)}
                aria-selected={selected}
                className={'rota-option' + (i === cursor ? ' active' : '') +
                  (selected ? ' rota-option-chosen' : '') + (o.overnight ? ' is-night' : '') +
                  (o.color ? ' is-tinted' : '') + (o.abs ? ' is-abs' : '')}
                style={o.abs ? { '--abs-ink': absenceHue(o.abs) } : tintStyle(o)}
                onMouseEnter={() => setCursor(i)}
                onMouseDown={e => e.preventDefault()}   // keep focus so the panel stays open
                onClick={() => choose(o)}>
                {selected && <span className="rota-option-tick">{Ic.check}</span>}
                {/* A separate 10px circle BEFORE the name, never a second rail: shape
                    discipline is what keeps the tint apart from the overnight edge
                    beside it. */}
                {o.color && <span className="rota-tint-dot" style={dotStyle(o.color)}/>}
                {o.abs && <span className="rota-option-kind" aria-hidden="true">{absenceGlyph[o.abs]}</span>}
                <span className="rota-option-name">{o.name}</span>
                {/* In the flow, immediately after the name -- NOT at the panel's right
                    edge. The panel is 226px wide inside an 85px column inside a
                    horizontally scrolling wrapper, so on a right-hand column it is
                    clipped at the right (27px over at column 3, 158px at column 6 --
                    pre-existing, and the same with or without this feature). Anything
                    pinned to that edge is the first thing to disappear, and this badge
                    carries the fact the manager most needs before choosing. It also
                    reads better here: the multiplier belongs beside the thing it
                    multiplies, not adrift at the far end of the row. */}
                {range && !o.abs && range.applicable > 1 &&
                  <span className="rota-option-multi">{range.applicable}&times;</span>}
                {!o.off && !o.abs && <span className="rota-option-hours">
                  {o.start_time}&ndash;{o.end_time}{o.overnight ? t.rotaPlusOne : ''}
                </span>}
                {/* The same badge the legend uses. A retired shift reaches
                    this list through a cell that is still assigned to it, which
                    is the only way it can be here at all -- so the picker is
                    exactly where the badge is needed most. */}
                {!o.off && !o.abs && o.active === false &&
                  <span className="rota-legend-retired">{t.rotaRetired}</span>}
              </button>
              </React.Fragment>
            );
          })}
          {/* What choosing here will do, before it does it. The second sentence
              exists because the first is not always the whole truth: a run can span
              a day somebody is away, and those days are skipped rather than refused --
              and a manager told "sets 6 days" who watches 5 change has been misled by
              omission. */}
          {range && range.applicable > 0 && (
            <div className="rota-picker-foot">
              {range.applicable < range.count
                ? t.rotaSelSetsSome.replace('{n}', range.applicable)
                                    .replace('{count}', range.count)
                : t.rotaSelSets.replace('{count}', range.count)}
            </div>
          )}
          {!absence && !range && shifts.length === 0 && (
            /* Reachable only in the narrow window where a manager has deleted
               every shift while the grid they came from is still on screen. NOT shown
               on an absent cell: that panel has three options and a foot that already
               explains itself, and "no shifts defined" there would read as a warning
               about the absence. */
            <div className="rota-picker-foot">{t.rotaNoShiftsHead}</div>
          )}
        </div>
      )}
    </div>
  );
}

/* The rota grid, in view mode.
   Split from SchedulePage so the read-only rendering has no `editing` branch
   anywhere inside it -- an anonymous visitor and a manager looking at the rota
   produce byte-identical DOM here, which is the only way to be sure the
   permission flag cannot produce a rendering difference nobody looked at. */

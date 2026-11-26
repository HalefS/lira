/* ── what a dialog owes the keyboard ──
   Eleven overlays in this app share one shape: a panel, a header, a close
   button. Ten of them declared aria-modal="true" and not one of them acted on
   it. Focus stayed on whatever opened the dialog, Tab walked straight out into
   the page behind, and Escape did nothing at all.

   That combination is worse than either half on its own. aria-modal tells
   assistive technology the rest of the page is gone; if Tab then puts the focus
   ring out there anyway, the reader has been told something the keyboard is
   contradicting. Withdrawing aria-modal was the other option and it is the
   weaker one -- it leaves screen-reader users free to browse a page that is
   visually covered. So the claim is kept and made true here, once, for all of
   them rather than in each.

   Three things that are load-bearing and not obvious:

   Only the topmost dialog reacts, checked by DOM order. The consumable picker,
   the TV-swap modal and the third-party modal all open from inside the issue
   form and live in the same document, so without this an Escape inside the
   picker would close the picker and the form behind it in the same keystroke.
   The `claimed` flag covers the case DOM order alone gets wrong: React commits
   the child's effect before the parent's, so the picker installs its listener
   first and handles Escape before the form's listener has run at all.

   An element that already holds focus is left there. `autoFocus` and
   `initialFocus` both run before this effect does, and several of these panels
   deliberately start somewhere other than the first control -- the Cancel in a
   destructive confirmation, the room field in the issue form -- so taking focus
   unconditionally would undo choices that were made on purpose.

   Focus is handed back on unmount. A dialog opened from the keyboard has to
   return the user to where they were, or closing one silently teleports them to
   the top of the document and they have to tab all the way down again. */
const DIALOG_FOCUSABLE = [
  'a[href]', 'button:not([disabled])', 'input:not([disabled])',
  'select:not([disabled])', 'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function useDialog({ onClose, initialFocus, preventClose = false }) {
  const panelRef = useRef(null);
  // The handler is held in a ref rather than listed as a dependency, because
  // every caller passes an inline arrow -- a fresh function each render -- and
  // depending on it would tear the listener down and rebuild it constantly.
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useEffect(() => {
    const panel = panelRef.current;
    if (!panel) return;
    const restoreTo = document.activeElement;

    // Hold the page still while the dialog is up. A wheel or a flick over a
    // fixed overlay does not stop at the overlay: it keeps scrolling the
    // nearest scroll ancestor behind it, which here is .content -- the app is
    // 100vh and clips, so the body never scrolls in the first place.
    //
    // Deliberately .content and not html/body: locking the root scroller
    // removes its scrollbar, which shifts the whole layout sideways the moment
    // a dialog opens. The native <dialog> route gets this for free by making
    // the page inert; these overlays are hand-built and predate it, so holding
    // the app's own scroll container is the equivalent that costs no layout.
    //
    // The previous value is saved rather than written back as '' so stacked
    // dialogs still work: the picker inside the issue form saves 'hidden' and
    // restores it, leaving the form's lock intact.
    const scrollers = [document.querySelector('.content')].filter(Boolean);
    const previousOverflow = scrollers.map(el => el.style.overflow);
    scrollers.forEach(el => { el.style.overflow = 'hidden'; });

    if (!panel.contains(document.activeElement)) {
      // The body before the whole panel. Every panel here opens with a close
      // button in its header, so focusing the first focusable outright would put
      // a dialog that has nothing to fill in -- the pickers -- on the X, which is
      // both a control nobody came here to use and a poor place to start reading
      // a list from. A caller that wants a specific field passes initialFocus.
      const body = panel.querySelector('.modal-body') || panel;
      const first = (initialFocus && panel.querySelector(initialFocus))
                 || body.querySelector(DIALOG_FOCUSABLE)
                 || panel.querySelector(DIALOG_FOCUSABLE);
      if (first) first.focus();
    }

    const onKeyDown = e => {
      if (e.claimedByDialog) return;
      const dialogs = document.querySelectorAll('[role="dialog"]');
      // Last in the DOM is the one drawn on top, and the only one entitled to
      // answer the keyboard.
      //
      // dialogs[length - 1], not dialogs[0][length - 1]: the second indexes the
      // first element's children by an integer, which an Element does not have, so
      // it is always undefined, never equals panel, and this returned early on
      // every key -- leaving all eleven dialogs unable to close on Escape or trap
      // Tab while appearing to.
      if (dialogs.length === 0 || dialogs[dialogs.length - 1] !== panel) return;
      e.claimedByDialog = true;

      if (e.key === 'Escape') {
        if (!preventClose && closeRef.current) closeRef.current();
        return;
      }
      if (e.key !== 'Tab') return;
      // Tab and Shift+Tab wrap, so the focus cannot leave through the page
      // behind. Only rendered controls count -- a chip that is not on screen is
      // not somewhere the keyboard can be sent. getClientRects rather than
      // offsetParent, which is null for anything position:fixed and would drop
      // real stops inside a panel that happens to be one.
      const stops = [...panel.querySelectorAll(DIALOG_FOCUSABLE)]
        .filter(el => el.getClientRects().length > 0);
      if (stops.length === 0) { e.preventDefault(); return; }
      const first = stops[0], last = stops[stops.length - 1];
      const at = document.activeElement;
      if (e.shiftKey ? (at === first || !panel.contains(at)) : (at === last || !panel.contains(at))) {
        e.preventDefault();
        (e.shiftKey ? last : first).focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      scrollers.forEach((el, i) => { el.style.overflow = previousOverflow[i]; });
      // Only take focus back if it belongs to this dialog. Two cases count: it is
      // still inside the panel, or it has fallen to <body> because the element
      // that held it was just removed -- which is what a browser does with focus
      // on a node it deletes, and which otherwise drops a keyboard user at the
      // top of the document. If focus has already moved somewhere real -- to the
      // field a sibling dialog put it in, say -- leave it alone; yanking it back
      // would be the bug rather than the fix.
      const at = document.activeElement;
      const ours = panel.contains(at) || at === document.body || at === null;
      if (restoreTo && restoreTo !== document.body && ours && restoreTo.isConnected) {
        restoreTo.focus();
      }
    };
    // initialFocus is a constant selector per caller; onClose is in a ref.
  }, [initialFocus, preventClose]);

  return panelRef;
}

/* The dialog opened by "Add consumables". Works on a draft copy of the selection;
   nothing changes on the issue until the user presses Done. */
function ConsumablePicker({ items, value, onCancel, onDone }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [draft, setDraft] = useState(value);   // [{itemId, quantity}]
  const [query, setQuery] = useState('');

  const panelRef = useDialog({onClose: onCancel});

  const byId = useMemo(()=>Object.fromEntries(draft.map(d=>[d.itemId,d])), [draft]);
  const q = query.trim().toLowerCase();
  const shown = q ? items.filter(it=>it.name.toLowerCase().includes(q)) : items;

  function toggle(id) {
    setDraft(d => d.some(x=>x.itemId===id) ? d.filter(x=>x.itemId!==id) : [...d, {itemId:id, quantity:1}]);
  }
  // Works from the latest state (not the last render), so fast clicks never lose a step.
  function bump(id, delta) {
    setDraft(d => d.flatMap(x => {
      if (x.itemId !== id) return [x];
      const qty = x.quantity + delta;
      return qty < 1 ? [] : [{...x, quantity: Math.min(qty, MAX_CONSUMABLE_QTY)}];  // stepping down from 1 un-selects it
    }));
  }

  return (
    <div className="modal-overlay picker-overlay" onClick={e=>e.target===e.currentTarget&&onCancel()}>
      <div className="modal picker-modal" ref={panelRef} role="dialog" aria-modal="true" aria-label={t.consumablePickerTitle}>
        <div className="modal-header">
          <div>
            <h2>{t.consumablePickerTitle}</h2>
            <div className="picker-sub">{t.consumablePickerSub}</div>
          </div>
          <button type="button" className="icon-btn" onClick={onCancel}>{Ic.x}</button>
        </div>
        <div className="modal-body">
          {items.length > 8 && (
            <div className="search-wrap" style={{width:'100%'}}>
              {Ic.search}
              {/* The placeholder is what a sighted reader sees, and a placeholder is not an
                accessible name -- it disappears on first keystroke and is not
                reliably announced at all. Same wording, now as a real name. */}
              <input aria-label={t.consumableSearch} placeholder={t.consumableSearch}
                value={query} onChange={e=>setQuery(e.target.value)} autoFocus/>
            </div>
          )}
          {items.length === 0 ? (
            <div className="empty-state" style={{padding:'28px 12px'}}>{Ic.box}<p>{t.consumableNone}</p></div>
          ) : shown.length === 0 ? (
            <div className="empty-state" style={{padding:'28px 12px'}}>{Ic.search}<p>{t.consumableNoMatch}</p></div>
          ) : (
            <div className="cpick-grid">
              {shown.map(it=>{
                const sel = byId[it.id];
                return (
                  <div key={it.id} role="button" tabIndex={0} aria-pressed={!!sel}
                    className={`cpick-card ${sel?'selected':''}`}
                    onClick={()=>toggle(it.id)}
                    onKeyDown={e=>{ if (e.key==='Enter' || e.key===' ') { e.preventDefault(); toggle(it.id); } }}>
                    <span className="cpick-check">{Ic.check}</span>
                    <ConsumableIcon name={it.icon} size={48}/>
                    <span className="cpick-name">{it.name}</span>
                    {/* Remaining stock, so it is obvious before saving that
                        something is about to be drawn down. Shortfall is
                        allowed — the work happened — but shown in red. */}
                    <span className="cpick-stock">
                      {t.inStock}: <strong style={{
                        color: (it.stock ?? 0) < 0 ? 'var(--red)'
                            : it.reorder_level > 0 && it.stock <= it.reorder_level ? 'var(--amber)'
                            : 'var(--gray-500)',
                      }}>{it.stock ?? 0}</strong>
                    </span>
                    {sel ? (
                      <div className="cpick-stepper" onClick={e=>e.stopPropagation()} onKeyDown={e=>e.stopPropagation()}>
                        <button type="button" aria-label="−" onClick={()=>bump(it.id, -1)}>{Ic.minus}</button>
                        <span>{sel.quantity}</span>
                        <button type="button" aria-label="+" onClick={()=>bump(it.id, +1)}>{Ic.plus}</button>
                      </div>
                    ) : (
                      <span className="cpick-hint">{t.consumableTapToAdd}</span>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
        <div className="modal-footer">
          <button type="button" className="btn btn-ghost" onClick={onCancel}>{t.cancel}</button>
          <button type="button" className="btn btn-primary" onClick={()=>onDone(draft)}>
            {Ic.check} {t.consumableDone}{draft.length > 0 ? ` (${draft.length})` : ''}
          </button>
        </div>
      </div>
    </div>
  );
}

/* The dialog opened by "TV swap" in the issue form: records that a working TV
   was taken from one room to another because the room's own set was unusable
   and there was no spare in stock.
   Works on a draft copy; nothing changes on the issue until Done. `key` is
   client-only and identifies a swap that doesn't exist on the server yet, so
   `id` stays the server's own row id (0 for a new one). */
function TVSwapModal({ swaps, currentRoom, onCancel, onDone }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [items, setItems] = useState(swaps);
  const [draft, setDraft] = useState(null);   // null = list view, otherwise the swap being added/edited
  const [err, setErr] = useState('');

  function blank() {
    return {key: `new-${Date.now()}-${Math.random().toString(36).slice(2,7)}`, id: 0,
            fromRoom: '', toRoom: currentRoom || '', notes: ''};
  }
  // Adding a swap needs its own trigger. It used to live in the footer, which
  // meant the only way out of the list view was Done — and Done discards,
  // so a swap added from there could never be kept.
  function startAdd()   { setDraft(blank()); setErr(''); }
  function startEdit(s) { setDraft({...s});  setErr(''); }
  function cancelEdit() { setDraft(null);    setErr(''); }
  function patch(k,v)   { setDraft(d=>({...d,[k]:v})); setErr(''); }

  function remove(key) {
    setItems(list => list.filter(x => x.key !== key));
    if (draft && draft.key === key) cancelEdit();
  }

  // Mirrors ValidateTVSwaps on the server, so the user is told here rather than
  // after a round-trip. The comparison is case-insensitive because room numbers
  // are typed by hand and "1324" / " 1324 " are the same room.
  function save() {
    const from = draft.fromRoom.trim();
    const to   = draft.toRoom.trim();
    if (!from)              return setErr(t.tvSwapErrNoFrom);
    if (!to)                return setErr(t.tvSwapErrNoTo);
    if (from.toLowerCase() === to.toLowerCase()) return setErr(t.tvSwapErrSame);
    const dup = items.some(x => x.key !== draft.key
      && x.fromRoom.trim().toLowerCase() === from.toLowerCase()
      && x.toRoom.trim().toLowerCase()   === to.toLowerCase());
    if (dup)                return setErr(t.tvSwapErrDup);
    if (items.length >= MAX_TV_SWAPS)  return setErr(t.tvSwapErrTooMany);

    const saved = {...draft, fromRoom: from, toRoom: to, notes: draft.notes.trim()};
    setItems(list => list.some(x => x.key === saved.key)
      ? list.map(x => x.key === saved.key ? saved : x)
      : [...list, saved]);
    cancelEdit();
  }

  // Escape means "back out of what I am in", not "close": from the editor it
  // returns to the list, and only from the list does it close the dialog.
  const panelRef = useDialog({onClose: () => draft ? cancelEdit() : onCancel()});

  function renderList() {
    return (
      <div className="modal-body">
        {/* Adding lives above the list, not in the footer, so the footer can
            commit the list. With it in the footer there was no way to add a
            swap and then keep it. */}
        <button type="button" className="btn btn-ghost" style={{marginBottom:12}}
          onClick={startAdd} disabled={items.length >= MAX_TV_SWAPS}>
          {Ic.plus} {t.addTvSwap}
        </button>
        {items.length === 0 ? (
          <div className="empty-state" style={{padding:'28px 12px'}}>{Ic.tv}<p>{t.tvSwapNone}</p></div>
        ) : items.map(s=>(
          <div className="tp-row" key={s.key}
               onClick={()=>startEdit(s)}
               role="button" tabIndex={0}
               onKeyDown={e=>{ if (e.key==='Enter') startEdit(s); }}
               style={{cursor:'pointer'}}>
            <span className="tp-row-icon">{Ic.tv}</span>
            <div style={{minWidth:0,flex:1}}>
              <div className="tp-row-title">
                <strong>{s.fromRoom}</strong>
                <span style={{color:'var(--gray-400)',margin:'0 6px'}}>{'\u2194'}</span>
                <strong>{s.toRoom}</strong>
              </div>
              {s.notes && <div className="tp-row-sub">{s.notes}</div>}
            </div>
            <button type="button" className="icon-btn" aria-label={t.tvSwapRemove}
              onClick={e=>{ e.stopPropagation(); remove(s.key); }}>{Ic.trash}</button>
          </div>
        ))}
        {err && <div style={{fontSize:12.5,color:'var(--red)',marginTop:8}}>{err}</div>}
      </div>
    );
  }

  function renderEditor() {
    return (
      <div className="modal-body">
        <div className="form-group">
          <label htmlFor="tvswap-from">{t.tvSwapFrom}</label>
          <input id="tvswap-from" className="form-control" value={draft.fromRoom} maxLength={100} autoFocus
            placeholder={t.tvSwapFromPlaceholder}
            onChange={e=>patch('fromRoom', e.target.value)}
            onKeyDown={e=>{ if (e.key==='Enter') save(); }}/>
        </div>
        <div className="form-group">
          <label htmlFor="tvswap-to">{t.tvSwapTo}</label>
          <input id="tvswap-to" className="form-control" value={draft.toRoom} maxLength={100}
            placeholder={currentRoom || t.tvSwapToPlaceholder}
            onChange={e=>patch('toRoom', e.target.value)}
            onKeyDown={e=>{ if (e.key==='Enter') save(); }}/>
        </div>
        <div className="form-group">
          <label htmlFor="tvswap-notes">{t.tvSwapNotes}</label>
          <input id="tvswap-notes" className="form-control" value={draft.notes} maxLength={500}
            placeholder={t.tvSwapNotesPlaceholder}
            onChange={e=>patch('notes', e.target.value)}
            onKeyDown={e=>{ if (e.key==='Enter') save(); }}/>
        </div>
        {/* Which room was actually reported broken is usually the destination,
            so calling that out saves re-reading the two fields. */}
        <div style={{fontSize:12,color:'var(--gray-400)'}}>
          {t.tvSwapHint}
        </div>
        {err && <div style={{fontSize:12.5,color:'var(--red)',marginTop:8}}>{err}</div>}
      </div>
    );
  }

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && onCancel()}>
      <div className="modal picker-modal" ref={panelRef} role="dialog" aria-modal="true" aria-label={t.addTvSwap}>
        <div className="modal-header">
          <div>
            <h2>{draft ? t.editTvSwap : t.addTvSwap}</h2>
            <div className="picker-sub">{draft ? t.tvSwapEditSub : t.tvSwapSub}</div>
          </div>
          <button type="button" className="icon-btn" onClick={onCancel}>{Ic.x}</button>
        </div>
        {draft ? renderEditor() : renderList()}
        <div className="modal-footer">
          {draft ? (
            <>
              <button type="button" className="btn btn-ghost" onClick={cancelEdit}>{t.cancel}</button>
              <button type="button" className="btn btn-primary" onClick={save}>{Ic.check} {t.saveChanges}</button>
            </>
          ) : (
            <>
              <button type="button" className="btn btn-ghost" onClick={onCancel}>{t.cancel}</button>
              <button type="button" className="btn btn-primary" onClick={()=>onDone(items)}>
                {Ic.check} {t.consumableDone}{items.length > 0 ? ` (${items.length})` : ''}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/* The dialog opened by "Third-party support" in the issue form. Works on a
   draft copy of the handovers; nothing changes on the issue until Done.
   `key` is client-only and identifies a handover that doesn't exist on the
   server yet, so `id` stays the server's own row id (0 for a new one). */
function SupportRequestModal({ requests, companies = [], onCancel, onDone }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [items, setItems] = useState(requests);
  const [draft, setDraft] = useState(null);   // null = list view, otherwise the handover being added/edited
  const [err, setErr] = useState('');

  // Escape from the handover being edited goes back to the list, which is what
  // the overlay click and the close button do from the same place.
  const panelRef = useDialog({onClose: () => draft ? cancelEdit() : onCancel()});

  // A "YYYY-MM-DDTHH:MM" value in the browser's own timezone, matching the
  // format the datetime-local inputs and the API use.
  function localDateTimeValue(d) {
    const pad = n => String(n).padStart(2,'0');
    return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  function companyFor(slug) {
    return (companies || []).find(c => c.slug === slug);
  }

  // Companies offered in the picker: active ones, plus the one already on this
  // handover even if it was deactivated after the fact.
  const choices = (companies || []).filter(c => c.active || (draft && c.slug === draft.company));

  // A company's handover form is its active field definitions. Inactive ones
  // stay on the handover itself and are never offered for a new entry, so old
  // history keeps its values without cluttering today's form.
  function fieldsFor(slug) {
    const c = companyFor(slug);
    return c && Array.isArray(c.fields) ? c.fields.filter(f => f && f.key) : [];
  }

  function valueOf(s, key) {
    const use = (s.fields || []).find(f => f.key === key);
    return use ? (use.value || '') : '';
  }

  function withFieldValue(list, key, value) {
    const exists = list.some(f => f.key === key);
    return exists ? list.map(f => f.key === key ? {...f, value} : f) : [...list, {key, value}];
  }

  // The tracking reference a handover is known by. Used to catch the same
  // number being entered twice on one issue, before the server has to.
  function referenceValue(s) {
    for (const def of fieldsFor(s.company)) {
      if (!def.active || def.type !== 'reference') continue;
      const value = valueOf(s, def.key).trim();
      if (value) return value.toLowerCase();
    }
    return '';
  }

  function blank() {
    // We know when we are reporting it — it is now — so the start is prefilled
    // rather than left as one more empty field to discover. The company is
    // deliberately not chosen: the handover form depends on it.
    return {key: `new-${Date.now()}-${Math.random().toString(36).slice(2,7)}`, id: 0,
            company: '', companyName: '', companyColor: '', status: 'pending', fields: [],
            startedAt: localDateTimeValue(new Date()), resolvedAt: '', notes: ''};
  }
  function startAdd()   { setDraft(blank());            setErr(''); }
  function startEdit(s) {
    const c = companyFor(s.company);
    setDraft({...s,
      fields: Array.isArray(s.fields) ? s.fields.map(f => ({...f, value: f.value || ''})) : [],
      companyName:  c ? c.name  : (s.companyName || s.company),
      companyColor: c ? c.color : (s.companyColor || 'gray'),
    });
    setErr('');
  }
  function cancelEdit() { setDraft(null);               setErr(''); }
  function patch(k,v)   { setDraft(d=>({...d,[k]:v})); setErr(''); }
  function patchField(key, value) {
    setDraft(d=>({...d, fields: withFieldValue(d.fields || [], key, value)}));
    setErr('');
  }

  // Whole minutes between the two moments, or null when either is missing.
  const elapsed = useMemo(()=>{
    if (!draft || !draft.startedAt) return null;
    const start = new Date(draft.startedAt).getTime();
    const end   = draft.resolvedAt ? new Date(draft.resolvedAt).getTime() : Date.now();
    if (isNaN(start) || isNaN(end)) return null;
    const mins = Math.round((end - start) / 60000);
    return mins < 0 ? null : mins;
  }, [draft]);

  // Mirrors ValidateSupportRequests on the server: the selected company's
  // definitions decide which values are needed and how they are checked.
  function commit() {
    if (!draft.company) return setErr(t.supportChooseCompany);
    for (const def of fieldsFor(draft.company)) {
      if (!def.active) continue;
      const value = valueOf(draft, def.key).trim();
      const needed = def.required || (def.type === 'confirmation' && draft.status === 'solved');
      if (!value) {
        if (needed) return setErr(t.supportFieldNeeded.replace('{field}', def.label));
        continue;
      }
      if (value.length > 100) return setErr(t.supportFieldTooLong.replace('{field}', def.label));
      if (def.type === 'date' && !/^\d{4}-\d{2}-\d{2}$/.test(value)) {
        return setErr(t.supportFieldInvalidDate.replace('{field}', def.label));
      }
      if (def.type === 'number' && !/^-?\d+(?:\.\d+)?$/.test(value)) {
        return setErr(t.supportFieldInvalidNumber.replace('{field}', def.label));
      }
      if (def.type === 'reference') {
        const key = value.toLowerCase();
        if (items.some(s => s.key !== draft.key && referenceValue(s) === key)) {
          return setErr(t.supportDuplicateReference);
        }
      }
    }
    if (!draft.startedAt) return setErr(t.supportNeedsReported);
    if (draft.status==='solved' && !draft.resolvedAt) return setErr(t.supportNeedsResolved);
    if (draft.startedAt && draft.resolvedAt && draft.resolvedAt < draft.startedAt) {
      return setErr(t.supportResolvedBefore);
    }
    const saved = {
      ...draft,
      // Keep values for definitions a manager has since removed: they are part
      // of the handover's history even though the form no longer asks for them.
      fields: (draft.fields || []).map(f => ({...f, value: String(f.value || '').trim()})),
      notes: (draft.notes||'').trim(),
    };
    setItems(list=> list.some(s=>s.key===draft.key) ? list.map(s=>s.key===draft.key?saved:s) : [...list, saved]);
    cancelEdit();
  }

  function remove(s) {
    setItems(list=>list.filter(x=>x.key!==s.key));
  }

  // A handover's fields belong to one company, so changing it starts a clean
  // form. Editing the same company never clears anything, including the values
  // of fields that are no longer offered.
  function setCompany(slug) {
    const c = companyFor(slug);
    setDraft(d => {
      if (!d || d.company === slug) return d;
      return {...d, company: slug,
        companyName:  c ? c.name  : slug,
        companyColor: c ? c.color : 'gray',
        fields: []};
    });
    setErr('');
  }

  // Marking a handover OK is the moment a solution arrived, so offer "now" as
  // the answer rather than making it be looked up.
  function markSolvedNow() {
    patch('status', 'solved');
    patch('resolvedAt', localDateTimeValue(new Date()));
  }

  // The second line of a handover row: every configured value that was filled
  // in, including one for a field the manager has since removed. Returns null
  // when there is genuinely nothing to add, so the row collapses.
  function detail(s) {
    const parts = fieldsFor(s.company)
      .map(f => ({label: f.label, value: valueOf(s, f.key).trim()}))
      .filter(x => x.value)
      .map(x => `${x.label}: ${x.value}`);
    return parts.length ? parts.join(' · ') : (s.notes || null);
  }

  return (
    <div className="modal-overlay picker-overlay" onClick={e=>e.target===e.currentTarget&&(draft?cancelEdit():onCancel())}>
      <div className="modal picker-modal" ref={panelRef} role="dialog" aria-modal="true" aria-label={t.thirdPartyTitle}>
        <div className="modal-header">
          <div>
            <h2>{t.thirdPartyTitle}</h2>
            <div className="picker-sub">{t.thirdPartySub}</div>
          </div>
          <button type="button" className="icon-btn" onClick={()=>draft?cancelEdit():onCancel()}>{Ic.x}</button>
        </div>
        <div className="modal-body">
          {draft ? (
            <>
              <div className="form-group">
                <label htmlFor="tp-company">{t.company}</label>
                <select id="tp-company" className="form-control" value={draft.company}
                  aria-invalid={!!err && !draft.company} onChange={e=>setCompany(e.target.value)}>
                  <option value="">{t.supportChooseCompany}</option>
                  {choices.map(c => (
                    <option key={c.slug} value={c.slug}>
                      {c.name}{!c.active ? ` · ${t.supportCompanyInactive}` : ''}
                    </option>
                  ))}
                </select>
                {!draft.company && <div className="field-hint">{t.supportChooseCompanyHelp}</div>}
              </div>

              {draft.company && (
                <div className="tp-fields">
                  <div className="tp-fields-h">{t.supportFieldsTitle}</div>
                  {fieldsFor(draft.company).filter(f => f.active).length === 0 ? (
                    <div className="field-hint">{t.supportNoFields}</div>
                  ) : fieldsFor(draft.company).filter(f => f.active).map(def => {
                    const id = `tp-field-${def.key}`;
                    const required = def.required || (def.type === 'confirmation' && draft.status === 'solved');
                    const inputType = def.type === 'date' ? 'date' : def.type === 'number' ? 'number' : 'text';
                    return (
                      <div className="form-group" key={def.key}>
                        <label htmlFor={id}>
                          {def.label}
                          {required && <span className="tp-req" aria-label={t.supportFieldRequired}> *</span>}
                        </label>
                        <input id={id} className="form-control" type={inputType}
                          step={inputType === 'number' ? 'any' : undefined}
                          placeholder={inputType === 'text' ? t.supportFieldPlaceholder : undefined}
                          aria-required={required}
                          value={valueOf(draft, def.key)}
                          onChange={e=>patchField(def.key, e.target.value)}/>
                      </div>
                    );
                  })}
                </div>
              )}

              <div className="form-group">
                <label htmlFor="tp-status">{t.supportStatus}</label>
                <select id="tp-status" className="form-control" value={draft.status} onChange={e=>patch('status', e.target.value)}>
                  <option value="pending">{t.supportPending}</option>
                  <option value="solved">{t.supportSolved}</option>
                </select>
              </div>

              {/* When we reported the problem, and when the company answered.
                  This is the company's turnaround, so it is wall-clock across
                  days rather than a shift length. */}
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="tp-reported-at">{t.supportReportedAt}</label>
                  <input id="tp-reported-at" type="datetime-local" className="form-control" value={draft.startedAt}
                    onChange={e=>patch('startedAt', e.target.value)}/>
                </div>
                <div className="form-group">
                  <label htmlFor="tp-resolved-at">{t.supportResolvedAt}</label>
                  <input id="tp-resolved-at" type="datetime-local" className="form-control" value={draft.resolvedAt}
                    onChange={e=>patch('resolvedAt', e.target.value)}/>
                </div>
              </div>

              {/* Live duration, so the effect of the two times above is
                  visible before saving. */}
              {elapsed != null && (
                <div style={{fontSize:12.5,color:'var(--gray-400)',marginTop:-8}}>
                  {t.supportDuration}: <strong style={{color:'var(--gray-600)'}}>{formatSupportDuration(elapsed)}</strong>
                </div>
              )}

              <div className="form-group">
                <label htmlFor="tp-notes">{t.supportNotes}</label>
                <input id="tp-notes" className="form-control" placeholder={t.supportNotesPlaceholder} value={draft.notes}
                  onChange={e=>patch('notes', e.target.value)}/>
              </div>

              {err && <div className="tp-error" role="alert">{err}</div>}
            </>
          ) : items.length === 0 ? (
            <div className="empty-state" style={{padding:'24px 12px'}}>{Ic.users}<p>{t.thirdPartyNoneLogged}</p></div>
          ) : (
            <div className="tp-list">
              {items.map(s=>(
                <div className="tp-row" key={s.key}>
                  <div className="tp-row-main">
                    <div className="tp-row-company">
                      <span className={`tp-company-dot ${s.companyColor || 'gray'}`}/>
                      {s.companyName || s.company}
                    </div>
                    {detail(s) && <div className="tp-row-detail">{detail(s)}</div>}
                    {s.durationMinutes != null && (
                      <div className="tp-row-detail">
                        {t.supportDuration}: <strong>{formatSupportDuration(s.durationMinutes)}</strong>
                      </div>
                    )}
                  </div>
                  <span className={`tp-row-status ${s.status}`}>{s.status==='solved' ? t.supportSolved : t.supportPending}</span>
                  <button type="button" className="icon-btn" title={t.editThirdParty} onClick={()=>startEdit(s)}>{Ic.edit}</button>
                  <button type="button" className="icon-btn danger" title={t.thirdPartyRemove} onClick={()=>remove(s)}>{Ic.trash}</button>
                </div>
              ))}
            </div>
          )}

          {!draft && (
            <button type="button" className="consumable-add-btn" onClick={startAdd} style={{width:'100%',padding:'9px 12px',fontSize:13}}>
              {Ic.plus} {t.addThirdParty}
            </button>
          )}
        </div>
        <div className="modal-footer">
          {draft ? (
            <>
              <button type="button" className="btn btn-ghost" onClick={cancelEdit}>{t.cancel}</button>
              <button type="button" className="btn btn-primary" onClick={commit}>{Ic.check} {t.saveChanges}</button>
            </>
          ) : (
            <>
              <button type="button" className="btn btn-ghost" onClick={onCancel}>{t.cancel}</button>
              <button type="button" className="btn btn-primary" onClick={()=>onDone(items)}>
                {Ic.check} {t.consumableDone}{items.length > 0 ? ` (${items.length})` : ''}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/* Asks before deleting an issue. Deleting takes the issue's consumables and its
   third-party handovers with it, so it is not something to do on a stray click.
   The subject line repeats which issue is about to go, because the confirm can
   be triggered from a row the user is looking at rather than at. */

function ConfirmDeleteIssue({ issue, onCancel, onConfirm, deleting }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];

  // Escape does nothing mid-delete: the request is already out and abandoning the
  // dialog would leave it with nowhere to report to.
  const panelRef = useDialog({onClose: deleting ? null : onCancel});

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && !deleting && onCancel()}>
      <div className="modal confirm-modal" ref={panelRef} role="dialog" aria-modal="true" aria-label={t.deleteIssueTitle}>
        <div className="modal-header">
          <h2>{t.deleteIssueTitle}</h2>
          <button type="button" className="icon-btn" onClick={onCancel} disabled={deleting}>{Ic.x}</button>
        </div>
        <div className="modal-body confirm-body">
          <span className="confirm-icon">{Ic.trash}</span>
          <div className="confirm-text">
            {t.deleteIssueBody}
            <span className="confirm-subject">
              {issue.apt||issue.dept} · {issue.type} — {issue.problem}
            </span>
          </div>
        </div>
        <div className="modal-footer">
          {/* Cancel takes the initial focus, not the delete button: Enter is
              often still held over from whatever the user did before, and a
              reflexive Enter should not destroy an issue. */}
          <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={deleting} autoFocus>{t.cancel}</button>
          <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={deleting}>
            {deleting ? t.deleting : <>{Ic.trash} {t.deleteIssueConfirm}</>}
          </button>
        </div>
      </div>
    </div>
  );
}

/*
  The start/end times to open the edit form with.

  Three cases, and the middle one is easy to confuse with the last:

  1. The issue has real times. Use them.
  2. The issue is Pending. It has no end time because the work is not finished,
     which is a real absence rather than missing data — so the end stays empty
     instead of being given a made-up one.
  3. The issue predates clock times. Nothing was ever recorded, so rebuild a
     placeholder pair ending "now" that reproduces the stored duration. Without
     this the duration line would be blank on every legacy issue.
*/
function initialClockTimes(issue) {
  // A value that already carries a date (new format) is used as-is. A legacy
  // clock-only value is attached to the issue's own day, so an overnight issue
  // keeps its duration instead of going negative.
  const attach = v => (v && v.includes('T')) ? v
    : (v ? `${issue.createdAt || localISODate(new Date())}T${v}` : '');
  const nextDay = iso => {
    const d = new Date(iso.slice(0,10) + 'T00:00:00');
    d.setDate(d.getDate() + 1);
    return localISODate(d);
  };

  if (issue.startTime && issue.endTime) {
    const s = attach(issue.startTime);
    let e = attach(issue.endTime);
    if (!issue.endTime.includes('T') && issue.endTime < issue.startTime) {
      e = `${nextDay(issue.startTime)}T${issue.endTime}`;
    }
    return { startTime: s, endTime: e };
  }
  if (issue.status === 'Pending') {
    return { startTime: issue.startTime ? attach(issue.startTime) : '', endTime: '' };
  }
  // Legacy: nothing ever recorded -- rebuild a placeholder pair ending now.
  const end = new Date();
  const start = new Date(end.getTime() - (issue.time || 0) * 60000);
  return { startTime: localDateTimeValue(start), endTime: localDateTimeValue(end) };
}

/* The apartment room field: a typeahead over the room inventory.

   Why this is not a <select>: the inventory is stored as ranges and may be
   hundreds of rooms, and the field's whole purpose is to narrow that by typing.
   A dropdown would have to be scrolled.

   Why the value is not simply what is typed: the room is the one field on this
   form that must be an existing room rather than free text, so typing does not
   commit anything. onChange is only called with a room that came back from the
   server, and typing clears the committed value -- which is what makes the form
   refuse to submit, since its validity test is on the committed value and not on
   the text in the box. The consequence is that "1214" typed and abandoned leaves
   an invalid form rather than an issue against a room called "1214 " that nobody
   can find again.

   The search is debounced and abandoned when the query changes, so a fast typist
   does not get a slow room's results painted over a fast room's. It fails
   silently: a network problem should not block logging a fault.
*/
function RoomPicker({ value, onChange, token, t, id }) {
  const [query, setQuery]   = useState(value || '');
  const [matches, setMatches] = useState([]);
  const [open, setOpen]     = useState(false);
  const [cursor, setCursor] = useState(0);
  // Which query the current matches belong to. A response for "13" must never be
  // painted as the answer to "7777", so the results carry the query they were
  // fetched for and are discarded if the box has moved on. The server's answer is
  // already scoped to the request that asked for it; this is about the gap between
  // asking and being told.
  const [answered, setAnswered] = useState('');
  // Whether the server had more matches than it returned. Kept apart from
  // matches.length because the count on screen is a number of results, not a
  // claim that those were all of them, and only the server knows which it was.
  const [more, setMore] = useState(false);
  const wrapRef = useRef(null);
  const listId  = 'room-options';

  // Keep the box in step with the committed value, which changes when an issue
  // loads into the form or when Maintenance opens this form with a room prefilled.
  useEffect(() => { setQuery(value || ''); }, [value]);

  useEffect(() => {
    const q = query.trim();
    if (!open || !q) { setMatches([]); setAnswered(''); setMore(false); return; }
    let live = true;
    const handle = setTimeout(() => {
      apiFetch(`/rooms/search?q=${encodeURIComponent(q)}`, {token})
        .then(res => {
          if (!live) return;
          setMatches(res.rooms || []);
          // Recorded so "no results" is only claimed for the query these results
          // were actually fetched with. Without it the box says a room is missing
          // from the inventory before the search for it has even been sent, which
          // is a confident answer to a question nobody has asked yet.
          setAnswered(q);
          setMore(!!res.more);
          setCursor(0);
        })
        .catch(() => { if (live) { setMatches([]); setAnswered(''); setMore(false); } });
    }, 180);
    return () => { live = false; clearTimeout(handle); };
  }, [query, open, token]);

  // Clicking anywhere else closes the panel. A mousedown listener rather than
  // blur, because blur fires before a click on an option has been handled and the
  // option would never be selected.
  useEffect(() => {
    if (!open) return;
    const away = e => { if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false); };
    // Capture phase, and it claims the event. This panel is inside a dialog, and
    // that dialog closes on Escape too: Escape should dismiss the dropdown and
    // leave the form it belongs to open. Registration order cannot be relied on
    // for this -- the panel is mounted with the form but its listener is attached
    // whenever `open` flips, which may be before or after the dialog's -- whereas
    // capture reliably runs ahead of a bubble-phase listener on document.
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

  const choose = room => {
    onChange(String(room));
    setQuery(String(room));
    setOpen(false);
  };

  const onKeyDown = e => {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') { setOpen(true); e.preventDefault(); }
      return;
    }
    if (e.key === 'ArrowDown') { setCursor(c => Math.min(c + 1, matches.length - 1)); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { setCursor(c => Math.max(c - 1, 0)); e.preventDefault(); }
    else if (e.key === 'Enter') {
      // Enter picks the highlighted room. With nothing highlighted it is left
      // alone, so the key still submits the form rather than silently choosing
      // the first match in a list nobody was looking at.
      if (matches[cursor] !== undefined) { choose(matches[cursor]); e.preventDefault(); }
    }
  };

  const typed   = query.trim();
  const chosen  = value || '';
  // Where the arrow keys currently are, as an id the combobox can point at. The
  // `.active` class already moves with the cursor, so a sighted reader knows;
  // a screen reader has only the DOM, and with nothing here it says nothing at
  // all while arrowing. Derived from the room rather than from the cursor index,
  // so the id belongs to that option for as long as it is on screen and the
  // attribute is not rewritten as the list re-renders under it.
  const optionId = room => `${listId}-${room}`;
  const activeId = (open && matches[cursor] !== undefined) ? optionId(matches[cursor]) : undefined;
  // Red only once there is text that is not the chosen room. Empty is neutral --
  // an empty required field is not an error until it has been filled in wrongly.
  const unmatched = typed !== '' && typed !== chosen;

  return (
    <div className={`room-picker${unmatched ? ' unmatched' : ''}`} ref={wrapRef}>
      <input id={id} className="form-control" autoComplete="off" role="combobox"
        aria-expanded={open} aria-controls={listId} aria-autocomplete="list"
        aria-activedescendant={activeId}
        placeholder={t.aptRoomPlaceholder}
        value={query}
        onChange={e => {
          const next = e.target.value;
          setQuery(next);
          setOpen(true);
          // Typing invalidates the choice: the room is only committed by picking
          // one from the list, so the box and the form cannot disagree.
          if (next.trim() !== chosen) onChange('');
        }}
        onFocus={() => { if (!chosen) setOpen(true); }}
        onKeyDown={onKeyDown}/>

      {open && (
        <div className="room-picker-panel" id={listId} role="listbox">
          {/* The "type to search" line is only for an empty box. Once there is a
              query, an empty result is a different statement -- that room is not
              in the inventory -- and it must not be confused with "nothing typed
              yet", which is what an earlier version showed and why typing 7777
              appeared to do nothing at all. */}
          {!typed && (
            <div className="room-picker-empty">
              {t.aptRoomTypeToSearch}
            </div>
          )}
          {typed && answered === typed && matches.length === 0 && (
            <div className="room-picker-empty">
              {t.aptRoomNoMatch.replace('{room}', typed)}
              {' '}{t.aptRoomAddHint}
            </div>
          )}
          {matches.map((room, i) => (
            <button type="button" key={room} role="option" id={optionId(room)}
              aria-selected={String(room) === chosen}
              className={`room-option${i === cursor ? ' active' : ''}${String(room) === chosen ? ' room-option-chosen' : ''}`}
              onMouseEnter={() => setCursor(i)}
              onMouseDown={e => e.preventDefault()}  // keep focus so the panel stays open
              onClick={() => choose(room)}>
              {String(room) === chosen && <span className="room-option-tick">{Ic.check}</span>}
              {String(room)}
            </button>
          ))}
          {typed && matches.length > 0 && (
            <div className="room-picker-foot">
              {more
                ? t.aptRoomMatchesMore.replace('{count}', matches.length)
                : t.aptRoomMatches.replace('{count}', matches.length)}
            </div>
          )}
          {/* Only once there is something to pick. `unmatched` on its own is true
              for the whole of every fresh query -- the search is debounced by 180ms
              and then in flight, and matches is empty throughout -- so asking the
              reader to pick from the list above printed "pick one of the rooms
              above" over an empty panel. Same reasoning as the two lines above it:
              nothing has been answered yet, so nothing should be claimed. */}
          {unmatched && matches.length > 0 && (
            <div className="room-picker-foot warn">{t.aptRoomMustPick}</div>
          )}
        </div>
      )}
    </div>
  );
}

/* One photo in a tile or in the viewer. The bytes are fetched here rather than
   put in an <img src> because the endpoint needs a bearer token, which an <img>
   cannot send. An object URL is created once per photo and revoked when this
   component unmounts, so a closed issue leaves no blobs behind. */
function PhotoImage({ issueId, photo, token, t, alt, className }) {
  const [url, setUrl] = useState('');
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let objectUrl = '';
    setUrl('');
    setFailed(false);

    function adopt(blob) {
      if (cancelled) return;
      objectUrl = URL.createObjectURL(blob);
      setUrl(objectUrl);
    }

    if (photo.file) {
      // Staged, not uploaded yet: show the local file. After an upload this
      // still points at the same picture, so a reopened viewer costs no fetch.
      adopt(photo.file);
    } else if (photo.id && token) {
      apiFetchBlob(`/issues/${issueId}/attachments/${photo.id}`, token)
        .then(adopt)
        .catch(() => { if (!cancelled) setFailed(true); });
    }

    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [issueId, photo.key, photo.file, photo.id, token]);

  if (failed) {
    return <span className={`ph-thumb-error ${className || ''}`} title={t.photoErrLoad} aria-hidden="true">{Ic.info}</span>;
  }
  if (!url) {
    return <span className={`ph-thumb-loading ${className || ''}`} aria-hidden="true"/>;
  }
  return <img className={`ph-thumb ${className || ''}`} src={url} alt={alt}
    width={photo.width || undefined} height={photo.height || undefined}/>;
}

function photoCountLabel(t, n) {
  return (n === 1 ? t.photoCountOne : t.photoCountMany).replace('{n}', n);
}

/* The issue table's photo marker. It sits in the problem cell next to the
   consumables, swaps and handovers, and carries no image -- twenty rows of
   thumbnails is not a table. */
function PhotoStrip({ photos, t }) {
  const n = (photos || []).length;
  if (!n) return null;
  const label = photoCountLabel(t, n);
  return (
    <div className="tp-strip">
      <span className="tp-strip-item photo" title={label}>
        <span aria-hidden="true">{Ic.camera} {n}</span>
        <span className="sr-only">{label}</span>
      </span>
    </div>
  );
}

/* Full-size viewer, opened from a tile. It is a sibling of the issue modal,
   not a child, so it is not clipped by the 520 px form. Removing a saved photo
   is manager-only and asks once; removing a staged one is just a discard and
   does not. */
function PhotoViewer({ issueId, photos, viewKey, token, t, currentUser, onClose, onRemove }) {
  const [confirming, setConfirming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [error, setError] = useState('');
  const panelRef = useDialog({ onClose, initialFocus: '#ph-viewer-close', preventClose: removing });

  const idx = (photos || []).findIndex(p => p.key === viewKey);
  const photo = idx >= 0 ? photos[idx] : null;

  useEffect(() => {
    setConfirming(false);
    setError('');
    setRemoving(false);
  }, [viewKey]);

  if (!photo) return null;

  const canRemove = photo.status !== 'saved' || currentUser.role === 'manager';

  async function remove() {
    if (photo.status !== 'saved') {
      onRemove(photo.key);
      return;
    }
    setRemoving(true);
    setError('');
    try {
      const res = await fetch(`${API}/issues/${issueId}/attachments/${photo.id}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` },
      });
      if (!res.ok) throw new Error('remove');
      onRemove(photo.key);
    } catch (_) {
      setError(t.photoRemoveFailed);
      setRemoving(false);
    }
  }

  return (
    <div className="modal-overlay picker-overlay"
      onClick={e => e.target === e.currentTarget && !removing && onClose()}>
      <div className="modal ph-viewer" ref={panelRef} role="dialog" aria-modal="true" aria-labelledby="ph-viewer-title">
        <div className="modal-header">
          <h2 id="ph-viewer-title">
            {t.photoViewerTitle.replace('{n}', idx + 1).replace('{total}', photos.length)}
          </h2>
          <button id="ph-viewer-close" className="icon-btn" onClick={onClose} aria-label={t.photoClose}>{Ic.x}</button>
        </div>
        <div className="modal-body ph-viewer-body">
          <PhotoImage issueId={issueId} photo={photo} token={token} t={t}
            alt={t.photoAlt.replace('{n}', idx + 1)} className="ph-viewer-img"/>
          {error && <div className="ph-error" role="alert">{Ic.info} <span>{error}</span></div>}
        </div>
        <div className="modal-footer ph-viewer-footer">
          {confirming ? (
            <>
              <span className="ph-confirm" role="alert">{t.photoRemoveConfirm}</span>
              <button className="btn btn-ghost" disabled={removing} onClick={() => setConfirming(false)}>{t.cancel}</button>
              <button className="btn btn-danger" disabled={removing} onClick={remove}>{t.photoRemove}</button>
            </>
          ) : (
            <>
              <button className="btn btn-ghost" onClick={onClose}>{t.photoClose}</button>
              {canRemove && (
                <button className="btn btn-danger"
                  onClick={() => photo.status === 'saved' ? setConfirming(true) : onRemove(photo.key)}>
                  {t.photoRemove}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function IssueModal({ issueMode, editIssue, prefill, currentUser, issueTypes, issuePresets, departments, connectaAgents, consumableItems, token, onClose, onSave, onPhotosChanged }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const isEdit = !!editIssue;
  // Whether the selected issue type offers to record equipment being moved
  // between rooms. Read off the type rather than matched by name, so renaming
  // a type cannot make the prompt disappear.
  const swapCapableTypes = useMemo(
    () => new Set((issueTypes || []).filter(it => it.tracks_swaps).map(it => it.name)),
    [issueTypes]
  );
  const typeNames = (issueTypes || []).map(it => it.name);
  if (isEdit && editIssue.type && !typeNames.includes(editIssue.type)) {
    typeNames.unshift(editIssue.type);
  }
  const deptNames = (departments || []).map(d => d.name);
  if (isEdit && editIssue.dept && !deptNames.includes(editIssue.dept)) {
    deptNames.unshift(editIssue.dept);
  }
  const agentNames = (connectaAgents || []).map(a => a.name);
  if (isEdit && editIssue.reportedByAgent && !agentNames.includes(editIssue.reportedByAgent)) {
    agentNames.unshift(editIssue.reportedByAgent);
  }
  if (isEdit && editIssue.confirmedByAgent && !agentNames.includes(editIssue.confirmedByAgent)) {
    agentNames.unshift(editIssue.confirmedByAgent);
  }
  const firstType = typeNames[0] || '';
  const catalogById = Object.fromEntries((consumableItems || []).map(ci => [ci.id, ci]));
  const nowStr = localDateTimeValue(new Date());
  const defaults = issueMode==='apt'
    ? {apt:'', type:firstType, problem:'', resolution:'', startTime: nowStr, endTime:'', status:'Ok', reportedByAgent:'', confirmedByAgent:'', falsePositive:false, consumables:[], supportRequests:[], tvSwaps:[]}
    : {dept:'',type:firstType, problem:'', resolution:'', startTime: nowStr, endTime:'', status:'Ok', reportedByAgent:'', confirmedByAgent:'', falsePositive:false, consumables:[], supportRequests:[], tvSwaps:[]};
  // prefill lets another page open this form with some of it already answered --
  // the Maintenance page does, so a fault found during a check does not have to
  // have its department and equipment type typed out again.
  const initial = prefill ? {...defaults, ...prefill} : defaults;
  const [form, setForm] = useState(isEdit ? {
    apt:        editIssue.apt||'',
    dept:       editIssue.dept||'',
    type:       editIssue.type,
    problem:    editIssue.problem,
    resolution: editIssue.resolution,
    // Use the actual start/end times the issue was logged with.
    ...initialClockTimes(editIssue),
    status:           editIssue.status,
    reportedByAgent:  editIssue.reportedByAgent || '',
    confirmedByAgent: editIssue.confirmedByAgent || '',
    falsePositive: !!editIssue.falsePositive,
    // Only items still in the catalog can be edited here. Usage recorded against
    // an item that has since been removed is kept on the server untouched.
    consumables: (editIssue.consumables || [])
      .filter(c => c.itemId != null && catalogById[c.itemId])
      .map(c => ({ itemId: c.itemId, quantity: c.quantity })),
    // Handovers to Telnet / Telefonica, as they are on the issue. The `key`
    // is client-only and derived from the server's row id, so the list can be
    // edited in place without an extra round-trip.
    supportRequests: (editIssue.supportRequests || []).map(s => ({...s, key: `srv-${s.id}`})),
    // TVs already moved for this issue. The `key` is client-only and derived
    // from the server's row id, so the list can be edited in place without an
    // extra round-trip.
    tvSwaps: (editIssue.tvSwaps || []).map(s => ({...s, key: `tvs-${s.id}`})),
  } : initial);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [supportOpen, setSupportOpen] = useState(false);
  const [swapOpen, setSwapOpen] = useState(false);
  const [presetId, setPresetId] = useState('');
  const [presetApplied, setPresetApplied] = useState('');

  // Photos are deliberately outside `form`. `form` is what addIssue/saveEdit
  // serialise, and a File object cannot go into that JSON. These tiles have
  // their own lifecycle: staged (ready), in flight (busy), stored (saved), or
  // failed with a retryable flag.
  const [photos, setPhotos] = useState(() => (isEdit && editIssue.attachments ? editIssue.attachments : []).map(p => ({
    key: `img-${p.id}`,
    status: 'saved',
    id: p.id,
    file: null,
    error: '',
    retryable: false,
    mimeType: p.mimeType,
    width: p.width,
    height: p.height,
  })));
  const [viewing, setViewing] = useState(null);   // photo key
  const [submitting, setSubmitting] = useState(false);
  const [upload, setUpload] = useState(null);     // {done, total}
  const [submitError, setSubmitError] = useState('');
  const [savedId, setSavedId] = useState(null);   // set once the issue exists
  const [photoNotice, setPhotoNotice] = useState('');
  const cameraRef = useRef(null);
  const galleryRef = useRef(null);
  const photoKeyRef = useRef(0);
  // Only offered on an issue type flagged tracks_swaps. Swaps already recorded
  // stay visible if the flag is later turned off, so the record isn't hidden
  // from someone editing that issue — but no new one can be added.
  const swapsOffered = swapCapableTypes.has(form.type);
  const showSwapBox = swapsOffered || form.tvSwaps.length > 0;
  const selectedConsumables = form.consumables
    .map(c => ({ ...c, ...catalogById[c.itemId] }))     // adds name + icon
    .filter(c => c.name);

  function set(k,v) { setForm(f=>({...f,[k]:v})); }

  // Presets belong to an issue type, so the selector only offers the ones for
  // the type currently chosen. Applying one replaces only the three fields it
  // describes -- location, clock, status, handovers, swaps and photos are the
  // technician's own and must survive.
  const presetsForType = (issuePresets || []).filter(p => p.issue_type === form.type);
  function applyPreset(id) {
    setPresetId(id);
    setPresetApplied('');
    if (!id) return;
    const preset = presetsForType.find(p => String(p.id) === String(id));
    if (!preset) return;
    const uses = (preset.consumables || [])
      .filter(c => catalogById[c.item_id])
      .map(c => ({ itemId: c.item_id, quantity: c.quantity }));
    setForm(f => ({
      ...f,
      problem: preset.problem || '',
      resolution: f.status === 'Pending' ? '' : (preset.resolution || ''),
      consumables: uses,
    }));
    setPresetApplied(t.presetApplied.replace('{name}', preset.name));
  }
  const duration = useMemo(()=>computeDurationMinutes(form.startTime, form.endTime), [form.startTime, form.endTime]);
  const timeRangeInvalid = form.startTime && form.endTime && duration === null;

  // Focus lands on the first field, which is the one that decides what this form
  // is for: the room, or the department. Not the close button, which is what the
  // first tab stop would otherwise be.
  const panelRef = useDialog({
    onClose,
    initialFocus: issueMode==='apt' ? '#apt-room' : '#issue-dept',
    // While the issue or its photos are being saved, Escape must not unmount
    // the form and lose the staged files and the id of an issue that already
    // exists.
    preventClose: submitting,
  });

  // A Pending issue has not been fixed yet, so the two fields that say the work
  // is done are taken away: the resolution is not the technician's to write, and
  // there is no end time for a job that has not ended. Switching back to Ok
  // clears the resolution too, so a real one has to be written from scratch
  // rather than the "still waiting" placeholder being kept and mistaken for one.
  const isPending = form.status === 'Pending';
  const resolution = isPending ? t.stillWaiting : form.resolution;

  function setStatus(next) {
    setForm(f => next === 'Pending'
      ? {...f, status: next, resolution: '', endTime: ''}
      : {...f, status: next, resolution: ''});
  }

  function addPhotos(fileList) {
    const files = Array.from(fileList || []);
    if (!files.length) return;
    const room = MAX_PHOTOS - photos.length;
    if (room <= 0) {
      setPhotoNotice(t.photosLimit.replace('{n}', MAX_PHOTOS));
      return;
    }
    const accepted = files.slice(0, room);
    setPhotoNotice(files.length > accepted.length ? t.photosLimit.replace('{n}', MAX_PHOTOS) : '');
    setPhotos(prev => [...prev, ...accepted.map(f => ({
      key: `new-${++photoKeyRef.current}`,
      status: 'ready',
      id: null,
      file: f,
      error: '',
      retryable: false,
    }))]);
  }

  function removePhoto(key) {
    const removed = photos.find(p => p.key === key);
    setPhotos(prev => prev.filter(p => p.key !== key));
    setViewing(null);
    // A saved photo was deleted on the server by the viewer before this runs;
    // the table's count is now stale.
    if (removed && removed.status === 'saved' && onPhotosChanged) onPhotosChanged();
    // The tile that had focus is gone. Hand focus to the next sensible place
    // rather than letting it fall to <body>.
    setTimeout(() => {
      const target = document.getElementById('ph-add-camera') || document.querySelector('.ph-tile');
      if (target) target.focus();
    }, 0);
  }

  // Upload one photo at a time, in order. Sequential rather than parallel: the
  // progress line stays honest, and a phone uplink split four ways is slower in
  // wall time than the same bytes sent one after another.
  async function runUploads(issueId) {
    const pending = photos.filter(p =>
      p.status === 'ready' || (p.status === 'error' && p.retryable && p.file));
    if (!pending.length) {
      return { ok: photos.every(p => p.status !== 'error'), failed: 0, total: 0 };
    }

    let failed = 0;
    for (let i = 0; i < pending.length; i++) {
      const p = pending[i];
      setUpload({ done: i + 1, total: pending.length });
      setPhotos(prev => prev.map(x => x.key === p.key
        ? {...x, status: 'busy', error: '', retryable: false}
        : x));
      try {
        const file = await shrinkImage(p.file);
        const att = await uploadIssuePhoto(issueId, file, token);
        setPhotos(prev => prev.map(x => x.key === p.key ? {
          ...x,
          status: 'saved',
          id: att.id,
          mimeType: att.mime_type,
          width: att.width,
          height: att.height,
          error: '',
          retryable: false,
        } : x));
      } catch (ex) {
        failed++;
        // 'type' and 'large' cannot be fixed by retrying, so those tiles keep
        // the file only so the user can see what failed, and the footer does
        // not offer Retry for them.
        const kind = (ex && ex.message === 'type') ? 'type'
          : (ex && (ex.message === 'large' || ex.status === 413)) ? 'large'
          : (ex && ex.status === 409) ? 'limit'
          : 'upload';
        const message = kind === 'type' ? t.photoErrType
          : kind === 'large' ? t.photoErrTooLarge
          : kind === 'limit' ? t.photosLimit.replace('{n}', MAX_PHOTOS)
          : t.photoErrUpload;
        setPhotos(prev => prev.map(x => x.key === p.key
          ? {...x, status: 'error', error: message, retryable: kind === 'upload'}
          : x));
      }
    }
    return { ok: failed === 0, failed, total: pending.length };
  }

  async function handleSubmit() {
    if (submitting) return;
    setSubmitError('');
    setSubmitting(true);

    let issueId = savedId;
    if (!issueId) {
      const id = await onSave({
        ...form,
        resolution: resolution,
        time: isPending ? 0 : duration,
        reportedByAgent: form.reportedByAgent || null,
        confirmedByAgent: form.confirmedByAgent || null,
      });
      if (!id) {
        // The issue POST/PATCH failed. Nothing was uploaded, the staged files
        // are intact, and pressing Save again is safe because no id was issued.
        setSubmitError(t.photoSaveFailed);
        setSubmitting(false);
        return;
      }
      setSavedId(id);
      issueId = id;
    }

    const result = await runUploads(issueId);
    setUpload(null);
    setSubmitting(false);
    if (onPhotosChanged) onPhotosChanged();

    if (result.ok) {
      onClose();
      return;
    }
    // Partial success: the issue exists, some photos do not. Keep the modal
    // open so Retry can upload only the failures -- and never re-POST the issue,
    // which savedId now prevents.
    setSubmitError(
      t.photoUploadPartial
        .replace('{failed}', result.failed)
        .replace('{total}', result.total)
      + ' ' + t.photoPartialHint
    );
  }

  // A pending issue is logged without an end time, so there is no duration to
  // compute and none is required; everything else still has to be filled in.
  const valid = (issueMode==='apt'
    ? form.apt && form.type && form.problem && resolution
    : form.dept && form.type && form.problem && resolution
  ) && (isPending || duration !== null);

  const failedPhotos = photos.filter(p => p.status === 'error');
  const retryableFailures = failedPhotos.some(p => p.retryable);

  // Recurring-issue check: once a location + type are picked, ask the
  // backend (debounced) whether this same issue type has already happened
  // there within the manager-configured window.
  const location = issueMode==='apt' ? form.apt : form.dept;
  const [dupeInfo, setDupeInfo] = useState(null); // {count, window_hours, issues}
  useEffect(() => {
    if (!location || !form.type || !token) { setDupeInfo(null); return; }
    const handle = setTimeout(() => {
      const params = new URLSearchParams({ mode: issueMode, location, type: form.type });
      if (isEdit) params.set('exclude_id', String(editIssue.id));
      apiFetch(`/issue-duplicates?${params.toString()}`, {token})
        .then(res => setDupeInfo(res))
        .catch(() => {}); // non-critical — fail silently, don't block logging
    }, 400);
    return () => clearTimeout(handle);
  }, [location, form.type, issueMode, token, isEdit, editIssue?.id]);

  return (
    <>
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget&&!submitting&&onClose()}>
      {/* aria-labelledby rather than aria-label because the title is already in
          the DOM and reads better with its own wording than a copy of it. */}
      <div className="modal issue-modal" ref={panelRef} role="dialog" aria-modal="true" aria-labelledby="issue-modal-title" aria-busy={submitting || undefined}>
        <div className="modal-header">
          <h2 id="issue-modal-title">{isEdit ? t.editIssue : (issueMode==='apt' ? t.logAptIssue : t.logDeptIssue)}</h2>
          <button className="icon-btn" onClick={onClose} disabled={submitting}>{Ic.x}</button>
        </div>
        <div className="modal-body">
          <div className="form-row">
            {issueMode==='apt' ? (
              <div className="form-group">
                <label htmlFor="apt-room">{t.aptNum}</label>
                {/* A room must be picked from the inventory, not typed. RoomPicker
                    only calls set('apt', ...) with a room the server confirmed
                    exists, so form.apt is never a string nobody chose. */}
                <RoomPicker id="apt-room" value={form.apt} token={token} t={t}
                  onChange={room => set('apt', room)}/>
              </div>
            ) : (
              <div className="form-group">
                <label htmlFor="issue-dept">{t.department}</label>
                <select id="issue-dept" className="form-control" value={form.dept} onChange={e=>set('dept',e.target.value)}>
                  <option value="">{t.selectDept}</option>
                  {deptNames.map(d=><option key={d}>{d}</option>)}
                </select>
              </div>
            )}
            <div className="form-group">
              <label htmlFor="issue-type">{t.issueType}</label>
              <select id="issue-type" className="form-control" value={form.type} onChange={e=>{ set('type',e.target.value); setPresetId(''); setPresetApplied(''); }}>
                {typeNames.length === 0 && <option value="">{t.selectDept}</option>}
                {typeNames.map(t2=><option key={t2}>{t2}</option>)}
              </select>
            </div>
          </div>
          {presetsForType.length > 0 && (
            <div className="form-group">
              <label htmlFor="issue-preset">{t.presetLabel}</label>
              <select id="issue-preset" className="form-control" value={presetId} onChange={e=>applyPreset(e.target.value)}>
                <option value="">{t.presetSelect}</option>
                {presetsForType.map(preset=><option key={preset.id} value={preset.id}>{preset.name}</option>)}
              </select>
              <div className="ph-hint">{t.presetHint}</div>
              {presetApplied && <div className="ph-note" role="status" aria-live="polite">{presetApplied}</div>}
            </div>
          )}
          {dupeInfo && dupeInfo.count > 0 && (
            <div style={{background:'var(--amber-light)',borderRadius:'var(--radius-md)',padding:'10px 14px',fontSize:13,color:'var(--amber)',display:'flex',alignItems:'flex-start',gap:8}}>
              <span style={{flexShrink:0,marginTop:1}}>{Ic.clock}</span>
              <div>
                <strong>{t.duplicateAlertTitle}.</strong>{' '}
                {duplicateAlertMessage(lang, dupeInfo.count, location, dupeInfo.window_hours)}
              </div>
            </div>
          )}
          <div className="form-group">
            <label htmlFor="issue-problem">{t.problemDesc}</label>
            <input id="issue-problem" className="form-control" placeholder={t.descPlaceholder} value={form.problem} onChange={e=>set('problem',e.target.value)} />
          </div>
          <div className="form-group">
            <label htmlFor="issue-resolution">{t.resolutionLabel}</label>
            <input id="issue-resolution" className="form-control" placeholder={t.resolutionPlaceholder}
              disabled={isPending} title={isPending ? t.stillWaiting : undefined}
              value={resolution} onChange={e=>set('resolution',e.target.value)} />
          </div>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="issue-start">{t.startTime}</label>
              <input id="issue-start" className="form-control" type="datetime-local" step="60" lang="sv-SE" value={form.startTime} onChange={e=>set('startTime',e.target.value)} />
            </div>
            <div className="form-group">
              <label htmlFor="issue-end">{t.endTime}</label>
              <input id="issue-end" className="form-control" type="datetime-local" step="60" lang="sv-SE" disabled={isPending}
                value={form.endTime} onChange={e=>set('endTime',e.target.value)} />
            </div>
          </div>
          <div className="form-group">
            <label htmlFor="issue-status">{t.statusLabel}</label>
            <select id="issue-status" className="form-control" value={form.status} onChange={e=>setStatus(e.target.value)}>
              <option value="Ok">{t.statusOk}</option>
              <option value="Pending">{t.statusPending}</option>
            </select>
          </div>
          <label className={`fp-check ${form.falsePositive ? 'checked' : ''}`}>
            <input id="issue-false-positive" type="checkbox" checked={form.falsePositive} onChange={e=>set('falsePositive', e.target.checked)}/>
            <span className="fp-box">{Ic.check}</span>
            <span>
              <span className="fp-title">{t.falsePositiveLabel}</span>
              <span className="fp-sub">{t.falsePositiveHint}</span>
            </span>
          </label>
          {/* Photos: the fault as found and the fix as left. They sit with the
              fields that describe the issue rather than just above the footer,
              where a filled strip would park itself in front of the save
              button. No collapsible section: the empty state is one row, and a
              disclosure hides the only control that makes evidence get added. */}
          <div className="form-group">
            <div className="form-group-h" id="issue-photos-h">
              {t.photosLabel}
              {photos.length > 0 && <span className="ph-count">{photos.length} / {MAX_PHOTOS}</span>}
            </div>
            <div className={`ph-box ${photos.length ? 'has-items' : ''}`}
                 role="group" aria-labelledby="issue-photos-h">
              {photos.length > 0 && (
                <div className="ph-strip">
                  {photos.map((p, i) => (
                    <button type="button" key={p.key} className={`ph-tile ${p.status}`}
                      title={p.error || undefined}
                      onClick={() => setViewing(p.key)}
                      aria-label={t.photoOpen.replace('{n}', i + 1)}>
                      <PhotoImage issueId={savedId || editIssue?.id} photo={p}
                        token={token} t={t} alt=""/>
                    </button>
                  ))}
                </div>
              )}
              {photos.length < MAX_PHOTOS && (
                <div className="ph-actions">
                  <button type="button" id="ph-add-camera" className="consumable-add-btn"
                    onClick={() => cameraRef.current && cameraRef.current.click()}>
                    {Ic.camera} {t.photoTake}
                  </button>
                  <button type="button" className="consumable-add-btn"
                    onClick={() => galleryRef.current && galleryRef.current.click()}>
                    {Ic.upload} {t.photoChoose}
                  </button>
                </div>
              )}
              {photos.length >= MAX_PHOTOS && (
                <div className="ph-hint">{t.photosLimit.replace('{n}', MAX_PHOTOS)}</div>
              )}
              {photos.length === 0 && (
                <div className="ph-hint">{t.photosHint.replace('{n}', MAX_PHOTOS)}</div>
              )}
              {upload && (
                <div className="ph-progress" role="status" aria-live="polite">
                  <div className="ph-progress-text">
                    {t.photoUploading.replace('{done}', upload.done).replace('{total}', upload.total)}
                  </div>
                  <div className="bar-track">
                    <div className="bar-fill"
                      style={{width: `${upload.total ? Math.round(upload.done / upload.total * 100) : 0}%`, background: 'var(--blue)'}}/>
                  </div>
                </div>
              )}
            </div>
            {/* The inputs are hidden; the buttons above are real buttons so they
                are keyboard reachable. capture=environment opens the camera on a
                phone and is ignored on desktop; the gallery input is separate
                because a technician may already have the photo. */}
            <input ref={cameraRef} type="file" accept="image/*" capture="environment" style={{display:'none'}}
              onChange={e => { addPhotos(e.target.files); e.target.value = ''; }}/>
            <input ref={galleryRef} type="file" accept="image/*" multiple style={{display:'none'}}
              onChange={e => { addPhotos(e.target.files); e.target.value = ''; }}/>
            {photoNotice && <div className="ph-note">{photoNotice}</div>}
            {failedPhotos.length > 0 && (
              <div className="ph-errors">
                {failedPhotos.map(p => (
                  <div key={p.key} className="ph-error" role="alert">{Ic.info} <span>{p.error}</span></div>
                ))}
              </div>
            )}
            {photos.length > 0 && (
              <span className="sr-only" role="status" aria-live="polite">{photoCountLabel(t, photos.length)}</span>
            )}
          </div>
          <div className="form-group">
            {/* A caption, not a label. What is below is a row of chips and one button,
                so there is no single control for a <label for> to name -- the
                group role carries the relationship instead. */}
            <div className="form-group-h" id="issue-consumables-h">{t.consumablesLabel}</div>
            <div className={`consumable-box ${selectedConsumables.length ? 'has-items' : ''}`}
                 role="group" aria-labelledby="issue-consumables-h">
              {selectedConsumables.map(c=>(
                <span className="consumable-chip" key={c.itemId}>
                  <ConsumableIcon name={c.icon} size={22}/>
                  <span className="consumable-chip-name">{c.name}</span>
                  {c.quantity > 1 && <span className="consumable-chip-qty">×{c.quantity}</span>}
                  <button type="button" className="consumable-chip-x" aria-label={t.consumableRemove}
                    onClick={()=>set('consumables', form.consumables.filter(x=>x.itemId!==c.itemId))}>{Ic.x}</button>
                </span>
              ))}
              <button type="button" className="consumable-add-btn" onClick={()=>setPickerOpen(true)}>
                {selectedConsumables.length ? Ic.edit : Ic.plus} {selectedConsumables.length ? t.editConsumables : t.addConsumables}
              </button>
            </div>
          </div>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="issue-agent-reported">{t.reportedByAgent}</label>
              <select id="issue-agent-reported" className="form-control" value={form.reportedByAgent} onChange={e=>set('reportedByAgent',e.target.value)}>
                <option value="">{t.noAgentSelected}</option>
                {agentNames.map(a=><option key={a}>{a}</option>)}
              </select>
            </div>
            <div className="form-group">
              <label htmlFor="issue-agent-confirmed">{t.confirmedByAgent}</label>
              <select id="issue-agent-confirmed" className="form-control" value={form.confirmedByAgent} onChange={e=>set('confirmedByAgent',e.target.value)}>
                <option value="">{t.noAgentSelected}</option>
                {agentNames.map(a=><option key={a}>{a}</option>)}
              </select>
            </div>
          </div>
          {/* Equipment moved between rooms. Only shown for an issue type flagged
              tracks_swaps (TV by default), which is how the question "did you
              have to take a TV from another room?" gets asked at all. */}
          {showSwapBox && (
            <div className="form-group">
              <div className="form-group-h" id="issue-swaps-h">{t.tvSwapLabel}</div>
              <div className={`tp-box ${form.tvSwaps.length ? 'has-items' : ''}`}
                   role="group" aria-labelledby="issue-swaps-h">
                {form.tvSwaps.map(s=>(
                  <span className="tp-chip tp-chip-swap" key={s.key}>
                    <span style={{display:'flex',alignItems:'center'}}>{Ic.tv}</span>
                    <span className="tp-chip-detail">
                      <strong>{s.fromRoom}</strong>
                      <span style={{color:'var(--gray-400)',margin:'0 4px'}}>{'\u2194'}</span>
                      <strong>{s.toRoom}</strong>
                    </span>
                    <button type="button" className="consumable-chip-x" aria-label={t.tvSwapRemove}
                      onClick={()=>set('tvSwaps', form.tvSwaps.filter(x=>x.key!==s.key))}>{Ic.x}</button>
                  </span>
                ))}
                {swapsOffered && (
                  <button type="button" className="consumable-add-btn" onClick={()=>setSwapOpen(true)}>
                    {form.tvSwaps.length ? Ic.edit : Ic.plus} {form.tvSwaps.length ? t.editTvSwaps : t.addTvSwap}
                  </button>
                )}
                {!swapsOffered && (
                  <span style={{fontSize:12,color:'var(--gray-400)'}}>{t.tvSwapFlagOff}</span>
                )}
              </div>
            </div>
          )}
          <div className="form-group">
            <div className="form-group-h" id="issue-thirdparty-h">{t.thirdPartyLabel}</div>
            <div className={`tp-box ${form.supportRequests.length ? 'has-items' : ''}`}
                 role="group" aria-labelledby="issue-thirdparty-h">
              {form.supportRequests.map(s=>(
                <span className={`tp-chip tp-chip-${s.company}`} key={s.key}>
                  <span className="tp-chip-company">{s.company==='telnet' ? t.companyTelnet : t.companyTelefonica}</span>
                  <span className="tp-chip-detail">
                    {s.company==='telnet' ? (s.technician || '—') : s.ticketId}
                  </span>
                  {s.status==='solved' && <span style={{color:'var(--green)'}}>{Ic.check}</span>}
                  <button type="button" className="consumable-chip-x" aria-label={t.thirdPartyRemove}
                    onClick={()=>set('supportRequests', form.supportRequests.filter(x=>x.key!==s.key))}>{Ic.x}</button>
                </span>
              ))}
              <button type="button" className="consumable-add-btn" onClick={()=>setSupportOpen(true)}>
                {form.supportRequests.length ? Ic.edit : Ic.plus} {form.supportRequests.length ? t.editThirdParty : t.addThirdParty}
              </button>
            </div>
          </div>
          {timeRangeInvalid ? (
            <div style={{fontSize:12.5,color:'var(--red)',marginTop:-8}}>{t.invalidTimeRange}</div>
          ) : duration !== null && !isPending ? (
            <div style={{fontSize:12.5,color:'var(--gray-400)',marginTop:-8}}>{t.duration}: <strong style={{color:'var(--gray-600)'}}>{formatSupportDuration(duration)}</strong></div>
          ) : null}
        </div>
        {submitError && (
          <div className="issue-submit-error" role="alert">{Ic.info}<span>{submitError}</span></div>
        )}
        <div className="modal-footer">
          <button className="btn btn-ghost" onClick={onClose} disabled={submitting}>{t.cancel}</button>
          {savedId && !submitting && !retryableFailures ? (
            <button className="btn btn-primary" onClick={onClose}>{t.photoClose}</button>
          ) : (
            <button className="btn btn-primary" disabled={submitting || (!savedId && !valid)} onClick={handleSubmit}>
              {submitting ? t.issueSaving
                : savedId ? <>{Ic.upload} {t.photosRetry}</>
                : <>{Ic.check} {isEdit ? t.saveChanges : t.logIssue}</>}
            </button>
          )}
        </div>
      </div>
    </div>
    {pickerOpen && (
      <ConsumablePicker items={consumableItems || []} value={form.consumables}
        onCancel={()=>setPickerOpen(false)}
        onDone={sel=>{ set('consumables', sel); setPickerOpen(false); }}/>
    )}
    {swapOpen && (
      <TVSwapModal swaps={form.tvSwaps} currentRoom={location}
        onCancel={()=>setSwapOpen(false)}
        onDone={sel=>{ set('tvSwaps', sel); setSwapOpen(false); }}/>
    )}
    {supportOpen && (
      <SupportRequestModal requests={form.supportRequests}
        onCancel={()=>setSupportOpen(false)}
        onDone={sel=>{ set('supportRequests', sel); setSupportOpen(false); }}/>
    )}
    {viewing && (
      <PhotoViewer issueId={savedId || editIssue?.id} photos={photos} viewKey={viewing}
        token={token} t={t} currentUser={currentUser}
        onClose={()=>setViewing(null)} onRemove={removePhoto}/>
    )}
    </>
  );
}


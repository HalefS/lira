function CapturePage({ token, issueTypes, departments, currentUser, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];

  const [mode, setMode] = useState('apt');
  const [location, setLocation] = useState('');
  const [type, setType] = useState('');
  const [problem, setProblem] = useState('');
  const [photos, setPhotos] = useState([]); // {id, blob, url}
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');
  const [pending, setPending] = useState([]);
  // The apartment room inventory, expanded from the ranges the server stores.
  // Loaded through apiFetch so the last list survives offline -- a room number
  // cannot be invented, the server checks it against this inventory.
  const [rooms, setRooms] = useState([]);
  const fileRef = useRef(null);

  const types = (issueTypes || []).map(it => it.name);
  const depts = (departments || []).map(d => d.name);
  const userID = currentUser && currentUser.id;

  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    apiFetch('/rooms', {token}).then(res => {
      if (cancelled) return;
      const numbers = [];
      (res.rooms || []).forEach(run => {
        for (let n = run.from_room; n <= run.to_room; n++) numbers.push(String(n));
      });
      numbers.sort((a, b) => Number(a) - Number(b));
      setRooms(numbers);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [token]);

  const refresh = useCallback(() => {
    if (userID == null) { setPending([]); return; }
    outboxAll(userID).then(setPending);
  }, [userID]);

  useEffect(() => { refresh(); }, [refresh]);

  // The sync engine announces every mutation and every completed run. One event
  // keeps the list, the sidebar badge and the form in step without a poll.
  useEffect(() => {
    const onChange = () => refresh();
    window.addEventListener('lira:outbox', onChange);
    return () => window.removeEventListener('lira:outbox', onChange);
  }, [refresh]);

  // Preview URLs are owned by this page; revoke them when it goes away so a
  // phone does not hold the blobs twice.
  const photosRef = useRef(photos);
  photosRef.current = photos;
  useEffect(() => () => {
    photosRef.current.forEach(p => { if (p.url) URL.revokeObjectURL(p.url); });
  }, []);

  async function addPhotos(files) {
    const room = MAX_PHOTOS - photos.length;
    if (!files || !files.length || room <= 0) return;
    setFormErr('');
    const added = [];
    for (const file of Array.from(files).slice(0, room)) {
      try {
        const shrunk = await shrinkImage(file);
        added.push({ id: newUUID(), blob: shrunk, url: URL.createObjectURL(shrunk) });
      } catch (ex) {
        setFormErr(ex && ex.message === 'large' ? t.photoErrTooLarge : t.photoErrType);
      }
    }
    if (added.length) setPhotos(p => [...p, ...added]);
    if (fileRef.current) fileRef.current.value = '';
  }

  function removePhoto(id) {
    setPhotos(p => {
      const gone = p.find(x => x.id === id);
      if (gone && gone.url) URL.revokeObjectURL(gone.url);
      return p.filter(x => x.id !== id);
    });
  }

  async function save(e) {
    e.preventDefault();
    setFormErr('');
    if (!location.trim()) { setFormErr(t.captureNeedLocation); return; }
    if (!type) { setFormErr(t.captureNeedType); return; }
    if (!problem.trim()) { setFormErr(t.captureNeedProblem); return; }
    if (userID == null) return;

    setBusy(true);
    const row = {
      id: newUUID(),
      user: String(userID),
      createdAt: isoNow(),
      mode,
      location: location.trim(),
      type,
      problem: problem.trim(),
      photos: photos.map(p => ({ id: p.id, blob: p.blob, type: 'image/jpeg', name: 'photo.jpg' })),
      state: 'pending',
      attempts: 0,
      uploadedPhotoIds: [],
      lastError: '',
      serverId: null,
    };

    try {
      await outboxAdd(row);
      photos.forEach(p => { if (p.url) URL.revokeObjectURL(p.url); });
      setPhotos([]);
      setLocation('');
      setProblem('');
      setType('');
      setToast(t.captureQueued);
      // Try immediately. If there is no signal the engine leaves it queued and
      // the online listener will pick it up later.
      try { window.dispatchEvent(new CustomEvent('lira:sync')); } catch (_) {}
    } catch (_) {
      // The browser's own storage error is not a sentence for a technician; the
      // translated one says what to do about it.
      setFormErr(t.captureSaveFailed);
    }
    setBusy(false);
  }

  async function discard(row) {
    if (!confirm(t.captureDiscardConfirm)) return;
    await outboxDelete(row.id);
    refresh();
  }

  function sendNow() {
    try { window.dispatchEvent(new CustomEvent('lira:sync')); } catch (_) {}
    setToast(t.captureSending);
  }

  return (
    <div className="capture-wrap">
      <form className="table-card capture-card" onSubmit={save}>
        <div className="capture-head">
          <div>
            <h2 className="capture-title">{t.captureTitle}</h2>
            <p className="capture-intro">{t.captureIntro}</p>
          </div>
        </div>

        <div className="capture-body">
          <div className="capture-modes" role="group" aria-label={t.captureModeLabel}>
            <button type="button" className={`chip ${mode==='apt'?'active':''}`}
              aria-pressed={mode==='apt'} onClick={()=>setMode('apt')}>{t.aptIssues}</button>
            <button type="button" className={`chip ${mode==='dept'?'active':''}`}
              aria-pressed={mode==='dept'} onClick={()=>setMode('dept')}>{t.deptIssues}</button>
          </div>

          <div className="form-group">
            <label htmlFor="capture-location">{mode==='apt' ? t.aptNum : t.department}</label>
            {mode==='apt' ? (
              <select id="capture-location" className="form-control" value={location}
                onChange={e=>setLocation(e.target.value)}>
                <option value="">{t.captureRoomSelect}</option>
                {rooms.map(r => <option key={r}>{r}</option>)}
              </select>
            ) : (
              <select id="capture-location" className="form-control" value={location}
                onChange={e=>setLocation(e.target.value)}>
                <option value="">{t.selectDept}</option>
                {depts.map(d => <option key={d}>{d}</option>)}
              </select>
            )}
          </div>

          <div className="form-group">
            <label htmlFor="capture-type">{t.issueType}</label>
            <select id="capture-type" className="form-control" value={type}
              onChange={e=>setType(e.target.value)}>
              <option value="">{t.selectDept}</option>
              {types.map(x => <option key={x}>{x}</option>)}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="capture-problem">{t.problemDesc}</label>
            <textarea id="capture-problem" className="form-control" rows={4} maxLength={500}
              placeholder={t.descPlaceholder} value={problem}
              onChange={e=>setProblem(e.target.value)} />
          </div>

          <div className="form-group">
            <div className="form-group-h">
              {t.photosLabel}
              {photos.length > 0 && <span className="ph-count">{photos.length} / {MAX_PHOTOS}</span>}
            </div>
            <div className={`ph-box ${photos.length ? 'has-items' : ''}`}>
              {photos.length > 0 && (
                <div className="ph-strip">
                  {photos.map(p => (
                    <button type="button" key={p.id} className="ph-tile"
                      onClick={()=>removePhoto(p.id)} aria-label={t.photoRemove} title={t.photoRemove}>
                      <img className="ph-thumb" src={p.url} alt="" />
                    </button>
                  ))}
                </div>
              )}
              {photos.length < MAX_PHOTOS && (
                <div className="ph-actions">
                  <button type="button" className="btn btn-ghost"
                    onClick={()=>{ if (fileRef.current) { fileRef.current.setAttribute('capture','environment'); fileRef.current.click(); } }}>
                    {Ic.camera} {t.photoTake}
                  </button>
                  <button type="button" className="btn btn-ghost"
                    onClick={()=>{ if (fileRef.current) { fileRef.current.removeAttribute('capture'); fileRef.current.click(); } }}>
                    {Ic.plus} {t.photoChoose}
                  </button>
                </div>
              )}
              <input ref={fileRef} type="file" accept="image/*" multiple hidden
                onChange={e=>addPhotos(e.target.files)} />
              <span className="ph-hint">{t.photosHint.replace('{n}', String(MAX_PHOTOS))}</span>
            </div>
          </div>

          {formErr && <div className="capture-err" role="alert">{formErr}</div>}
          {types.length === 0 && <div className="capture-note">{t.captureNoTypes}</div>}
          {mode==='apt' && rooms.length === 0 && <div className="capture-note">{t.captureNoRooms}</div>}

          <button className="btn btn-primary capture-save" type="submit"
            disabled={busy || types.length === 0 || (mode==='apt' && rooms.length === 0)} aria-busy={busy}>
            {busy ? t.issueSaving : t.captureSave}
          </button>
        </div>
      </form>

      <div className="table-card capture-card">
        <div className="capture-head">
          <h2 className="capture-title">{t.capturePendingTitle}</h2>
          {pending.length > 0 && (
            <span className="capture-pending-count">
              {t.capturePendingCount.replace('{count}', String(pending.length))}
            </span>
          )}
        </div>
        <div className="capture-body">
          {pending.length === 0 && <div className="capture-empty">{t.captureEmpty}</div>}
          {pending.map(row => (
            <div key={row.id} className="capture-row">
              <div className="capture-row-main">
                <div className="capture-row-title">
                  {row.mode==='apt' ? `${t.aptNum}${row.location}` : row.location} · {row.type}
                </div>
                <div className="capture-row-meta">
                  {new Date(row.createdAt).toLocaleString()}
                  {' · '}
                  {row.mode==='apt' ? t.aptIssues : t.deptIssues}
                  {row.photos && row.photos.length ? ' · ' + t.photoCountMany.replace('{n}', String(row.photos.length)) : ''}
                </div>
                <div className="capture-row-problem">{row.problem}</div>
                {row.state === 'rejected' && (
                  <div className="capture-row-bad">
                    {t.captureRejected.replace('{error}', row.lastError || '')}
                  </div>
                )}
                {row.state === 'needs-auth' && <div className="capture-row-bad">{t.captureNeedsAuth}</div>}
                {row.state !== 'rejected' && row.state !== 'needs-auth' && row.lastError && (
                  <div className="capture-row-wait">
                    {t.captureRetryHint.replace('{error}', row.lastError)}
                  </div>
                )}
              </div>
              <div className="capture-row-actions">
                {row.state !== 'rejected' && (
                  <button type="button" className="btn btn-ghost" onClick={sendNow}>{t.captureSendNow}</button>
                )}
                <button type="button" className="btn btn-ghost capture-discard" onClick={()=>discard(row)}>
                  {t.captureDiscard}
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}


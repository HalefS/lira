/* ════════════════ LCU TESTING PAGE ════════════════ */
/*
  A door card reader that has gone intermittent is put on trial for a week
  before it is scrapped. The test is holding an RFID card against it: a green
  light means it read the card, red means it did not. A reader that works on
  Monday can still be failing by Friday, hence seven days rather than one.

  The unit is the reader; its seven days are the log. The server decides who may
  answer (the technician who added it, or a manager) and sends can_answer so the
  controls are only drawn where they will actually work.
*/

// One cell per day of the trial. The dot is the reader's light on that day.
function LCUDayStrip({ unit, today, index = 0 }) {
  const lang = React.useContext(LangCtx);

  /* The lights come on in day order as the page is displayed. Keyed on the day
     count and on how many days carry a verdict, so it re-runs when the strip
     actually changes shape -- a window that grew, or a day just answered -- and
     not on every unrelated re-render of the card around it.

     `index` offsets each reader against the last, so a page with four readers on
     trial fills in one strip after another instead of all four at once. Capped,
     or a long list would leave the last card waiting seconds to start. */
  const answered = unit.days.filter(d => d.result).length;
  const stripRef = useAnimate((g, root) => {
    MOTION.light(g, root.querySelectorAll('.lcu-day.pass .lcu-day-light, .lcu-day.fail .lcu-day-light'),
      { delay: Math.min(index, 8) * 0.13 });
  }, [unit.days.length, answered]);

  return (
    <div className="lcu-strip" ref={stripRef}>
      {unit.days.map(day=>{
        const cls = ['lcu-day'];
        if (day.result === 'pass') cls.push('pass');
        else if (day.result === 'fail') cls.push('fail');
        if (day.day.slice(0,10) === today) cls.push('today');
        // A `date` column arrives from the server as midnight UTC. Handing that
        // straight to new Date() and reading getDate() back shows the PREVIOUS
        // day anywhere west of Greenwich, so the window would start a day early.
        // Building the local date from its parts keeps the label on the day the
        // server meant.
        const [y, m, dNum] = day.day.slice(0,10).split('-').map(Number);
        const asDate = new Date(y, m-1, dNum);
        return (
          <div key={day.day} className={cls.join(' ')}
            title={`${day.day.slice(0,10)}${day.result ? ' — ' + day.result : ''}`}>
            <span className="lcu-day-dow">{asDate.toLocaleDateString(lang==='pt'?'pt-PT':'en-GB',{weekday:'narrow'})}</span>
            <span className="lcu-day-date">{asDate.getDate()}</span>
            <span className="lcu-day-light"/>
          </div>
        );
      })}
    </div>
  );
}

function LCUUnitCard({ unit, today, onAnswer, onDelete, t, index }) {
  const [choosing, setChoosing] = useState(false);
  const [choice, setChoice]   = useState('');
  const [saving, setSaving]   = useState(false);
  const [err, setErr]         = useState('');

  const active = unit.status === 'active';
  const tested = unit.passes + unit.fails;
  const dayNo  = unit.days.findIndex(d=>d.day.slice(0,10) === today) + 1;
  // Only the days that have not happened yet count towards "left", and only
  // while the window is still open.
  const left  = active ? unit.days.filter(d=>d.day.slice(0,10) > today).length : 0;

  async function save(result) {
    setSaving(true); setErr('');
    try { await onAnswer(unit.id, result); setChoosing(false); setChoice(''); }
    catch(ex) { setErr(ex.message); }
    finally { setSaving(false); }
  }

  return (
    <div className="table-card" style={{padding:'18px 20px'}}>
      <div className="lcu-row">
        <div>
          <span className="lcu-serial">{unit.serial}</span>
          <div className="lcu-meta" style={{marginTop:5}}>
            {shortName(unit.added_by_name) && <span>{t.lcuAddedBy} {shortName(unit.added_by_name)}</span>}
            <span>{unit.starts_on.slice(0,10)} → {unit.ends_on.slice(0,10)}</span>
            {unit.note && <span>· {unit.note}</span>}
          </div>
        </div>
        <div style={{display:'flex',alignItems:'center',gap:8}}>
          {unit.status === 'passed' && <span className="badge badge-ok">{t.lcuPassed}</span>}
          {unit.status === 'failed' && <span className="badge" style={{background:'var(--red-light)',color:'var(--red)'}}>{t.lcuFailed}</span>}
          {active && <span className="badge badge-pending">{t.lcuOnTrial}</span>}
          {active && dayNo > 0 && <span className="lcu-meta">{t.lcuDay} {dayNo}/{unit.days.length}</span>}
        </div>
      </div>

      <LCUDayStrip unit={unit} today={today} index={index}/>

      <div className="lcu-row" style={{marginTop:16,marginBottom:0,alignItems:'center'}}>
        <div className="lcu-tally">
          <b>{unit.passes}</b> {t.lcuPassed.toLowerCase()} · <b>{unit.fails}</b> {t.lcuFailed.toLowerCase()}
          {left > 0 && <> · {left} {t.lcuDaysLeft.toLowerCase()}</>}
        </div>
        <div style={{display:'flex',gap:8}}>
          {active && unit.can_answer && !choosing && dayNo > 0 && (
            <button className="btn btn-primary" onClick={()=>setChoosing(true)}>
              {t.lcuRecordToday}
            </button>
          )}
          {unit.can_answer && (
            /* The confirm names the unit's own window rather than the current
               setting, so it stays truthful for a reader that was put on trial
               under a different length. */
            <button className="btn btn-ghost" style={{fontSize:12,padding:'6px 12px'}}
              onClick={()=>{ if (confirm(t.lcuDeleteConfirm.replace('{days}', unit.window_days || 7))) onDelete(unit.id); }}>
              {t.lcuDelete}
            </button>
          )}
        </div>
      </div>

      {err && <div className="auth-error" style={{marginTop:12}}>{err}</div>}

      {choosing && (
        <div style={{marginTop:14,paddingTop:14,borderTop:'1px solid var(--gray-200)'}}>
          <div className="lcu-howto" style={{marginBottom:12}}>
            {Ic.info}
            <span>{t.lcuHowToTest}</span>
          </div>
          <div className="lcu-choice">
            <button className={`lcu-choice-btn pass ${choice==='pass'?'on':''}`}
              onClick={()=>setChoice('pass')} disabled={saving}>
              <span className="lcu-bulb"/>{t.lcuPassed}
            </button>
            <button className={`lcu-choice-btn fail ${choice==='fail'?'on':''}`}
              onClick={()=>setChoice('fail')} disabled={saving}>
              <span className="lcu-bulb"/>{t.lcuNotPassed}
            </button>
          </div>
          <div style={{display:'flex',gap:8,marginTop:12,justifyContent:'flex-end'}}>
            <button className="btn btn-ghost" onClick={()=>{setChoosing(false);setChoice('');}} disabled={saving}>
              {t.cancel}
            </button>
            <button className="btn btn-primary" disabled={!choice || saving} onClick={()=>save(choice)}>
              {saving ? '…' : t.lcuSave}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/* One row of the resting history -- a trial that has ended.

   A table row, and specifically NOT an LCUUnitCard. A resolved unit's whole log is
   three numbers: passes, fails, window length. Seven coloured day-cells carry
   exactly those three numbers, so the strip is ninety pixels of picture around twenty
   pixels of information, and on a forty-row pool that is the difference between a
   scroll and a page.

   It also carries no answer control, because there is nothing left to answer. */
function LCURestingRow({ t, lang, unit, fresh, onDelete, busy }) {
  const [err, setErr] = useState('');

  const passed = unit.status === 'passed';
  // The window it actually ran, not the current setting -- a reader trialled for a
  // week stays a week long after the default moves to fourteen.
  const range = `${unit.starts_on.slice(0,10)} → ${unit.ends_on.slice(0,10)}`;
  const since = unit.pool_counted_at
    ? new Date(unit.pool_counted_at).toLocaleDateString(lang === 'pt' ? 'pt-PT' : 'en-GB',
        { day: '2-digit', month: 'short' })
    : '';

  async function remove() {
    setErr('');
    try { await onDelete(unit); }
    catch (ex) {
      // Row-local, not the page banner. The banner sits above the list, so a refusal
      // reported there is nine hundred pixels from the button that caused it.
      if (ex.status === 409) { setErr(t.lcuDeleteBlocked); return; }
      setErr(ex.message || '');
    }
  }

  return (
    <tr className={fresh ? 'row-enter' : undefined}>
      <td><span className="lcu-serial">{unit.serial}</span></td>
      <td>
        <span className={`badge lcu-verdict ${passed ? 'is-pass' : 'is-fail'}`}>
          {passed ? t.lcuPassed : t.lcuFailed}
        </span>
      </td>
      <td className="lcu-cell-dim">{range}</td>
      <td className="lcu-cell-fig"
        title={`${unit.passes} ${t.lcuPassed.toLowerCase()} · ${unit.fails} ${t.lcuFailed.toLowerCase()}`}>
        {unit.passes}/{unit.window_days}
      </td>
      <td className="lcu-cell-dim">
        {unit.resolved_at
          ? new Date(unit.resolved_at).toLocaleDateString(lang === 'pt' ? 'pt-PT' : 'en-GB',
              { day: '2-digit', month: 'short' })
          : '—'}
      </td>
      <td className="lcu-cell-recond">
        {unit.in_pool ? (
          <>
            <span className="lcu-pool-since">{t.lcuPoolSince.replace('{date}', since)}</span>
            {shortName(unit.added_by_name) && <span className="lcu-meta">{t.lcuAddedBy} {shortName(unit.added_by_name)}</span>}
          </>
        ) : (
          <span className="lcu-cell-none">—</span>
        )}
      </td>
      <td style={{textAlign:'right'}}>
        {/* Delete is offered ONLY when the reader is not in the pool, and the reason is
            not that deleting is forbidden -- it is not, with force -- but that on a
            pool-bound reader the button's most common outcome is a refusal. A control
            whose common outcome is an error is a bad control.

            It is still there for failed readers and for passed readers that predate the
            pool, so nothing that used to be deletable becomes quietly undeletable. */}
        {!unit.in_pool && unit.can_answer && (
          <button className="btn btn-ghost" style={{fontSize:12,padding:'6px 12px'}}
            disabled={busy}
            onClick={()=>{ if (confirm(t.lcuDeleteConfirm.replace('{days}', unit.window_days || 7))) remove(); }}>
            {t.lcuDelete}
          </button>
        )}
      </td>
      {err && (
        <tr className="lcu-err-row"><td colSpan={7}>
          <div className="auth-error" style={{margin:'0'}}>{err}</div>
        </td></tr>
      )}
    </tr>
  );
}

function LCUPage({ token, setToast, onLcuRequired, refreshKey, windowDays }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [err, setErr]         = useState('');
  const [adding, setAdding]   = useState(false);
  const [serial, setSerial]   = useState('');
  const [note, setNote]       = useState('');
  const [saving, setSaving]   = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [restBusy, setRestBusy] = useState(false);

  // The trial length is a manager setting, so the page copy cannot name a number.
  // 7 is the same default the server falls back to.
  const days = windowDays || 7;
  // Fills the {days} placeholder the shared strings carry. Small enough to run
  // on every render without mattering.
  const withDays = s => s.replace('{days}', days);

  /* The resting history, paged independently of the active list.

     A second usePagedList instance is safe because all of that hook's state is
     function-local -- no module state, no context, no singleton -- so the two carry
     separate limits and separate rows. They share filterKey deliberately: one reload
     after answering a day should drop BOTH back to a single page, and the reason the
     active list drops back is that its rows were fetched before the answer and the
     answer is not among them.

     It is also the request that PERFORMS the sweep. See cmd/api/lcu.go: this
     endpoint is the only place ResolveDue runs, precisely so that a credit made by
     any sweep can be reported to the next load of this page. */
  const restingList = usePagedList(limit => `/lcu/resting?limit=${limit}`, token, {
    rowsKey: 'units',
    filterKey: `${refreshKey}|${reloadKey}`,
    onError: ex => {
      if (ex.lcuRequired) { onLcuRequired(ex); return; }
      // Deliberately NOT setErr. The active list owns the page-level banner; two
      // failing requests must not stack two copies of the same message at the top.
    },
  });
  /* Readers on trial, paged. `refreshKey` is bumped when the daily prompt closes,
     so the list is re-read after an answer rather than left showing whatever it
     managed to fetch before the gate closed -- and it doubles as the filter key,
     so it also drops back to a single page rather than showing the answer under
     pages of rows fetched before it.

     THE FILTER KEY IS DELIBERATELY PAIRED WITH THE RESTING LIST'S ARRIVAL BELOW.

     ResolveDue -- the sweep that closes ended windows and credits the pool -- now
     runs on GET /v1/lcu/resting and nowhere else, so that a credit made by ANY
     sweep can be reported (cmd/api/lcu.go explains why /v1/lcu/today was the wrong
     home for it). The cost is that /vcu/units no longer sweeps, and a page that
     fetched both in parallel could read the active list BEFORE the sweep and render
     eight resolved readers as "On trial" -- which is exactly what the first
     screenshot showed.

     Making this list wait for the resting list is the fix. It costs one round trip
     of ordering on a page that is showing a week of history anyway, and it is the
     only thing here that is load-bearing rather than tidy: reversing these two
     requests reproduces the bug immediately. Hence the key, and hence this
     paragraph -- a bare `filterKey` that happens to encode "after" is the kind of
     line nobody dares touch.

     Adding a unit makes today's result owed straight away, so the reload that
     follows is itself refused. That is the gate working, not a failure, and it
     arrives here as an error rather than an empty list. */
  const paged = usePagedList(limit => `/lcu/units?limit=${limit}`, token, {
    rowsKey: 'units',
    filterKey: restingList.rows ? `${refreshKey}|${reloadKey}` : 'awaiting-sweep',
    onError: ex => {
      if (ex.lcuRequired) { onLcuRequired(ex); return; }
      setErr(ex.message);
    },
  });

  const units = paged.rows || [];
  const today = (paged.res && paged.res.today) || localISODate(new Date());
  // Cleared on a successful fetch, so a transient failure does not leave a red
  // line above a list that has since loaded perfectly well.
  useEffect(() => { if (paged.res) setErr(''); }, [paged.res]);


  const restRows = restingList.rows || [];
  const restRes  = restingList.res;
  const pool    = (restRes && restRes.reconditioned && restRes.reconditioned.pool) || 0;
  const yours   = (restRes && restRes.reconditioned && restRes.reconditioned.yours) || 0;
  const credits = (restRes && restRes.newly_reconditioned) || [];

  /* THE TOAST, GUARDED ON IDENTITY RATHER THAN ON "FIRST RENDER".

     loadMore refetches /lcu/resting with a bigger limit, and a sweep can genuinely
     happen between two of those fetches -- so newly_reconditioned can legitimately be
     non-empty again on a later load, not only on the first. Guarding on ids rather
     than on a once-flag therefore does the right thing in both cases, where "only
     toast on mount" would announce the same three readers twice and "only ever toast
     once" would stay silent about the fourth. */
  const announced = useRef({});
  useEffect(() => {
    if (!credits.length) return;
    const fresh = credits.filter(c => !announced.current[c.unit_id || c.serial]);
    if (!fresh.length) return;
    credits.forEach(c => { announced.current[c.unit_id || c.serial] = true; });
    setToast(fresh.length === 1
      ? t.lcuReconditionedOne
      : t.lcuReconditionedMany.replace('{count}', fresh.length));
  }, [restRes, setToast, t]);

  // The pulse is a one-shot flag rather than a class held for ever: the number stops
  // moving, so the highlight has to stop too or it reads as "still counting".
  const [poolFresh, setPoolFresh] = useState(false);
  useEffect(() => {
    if (!credits.length) return;
    setPoolFresh(true);
    const h = setTimeout(() => setPoolFresh(false), 800);
    return () => clearTimeout(h);
  }, [restRes]);
  useEffect(() => { if (!restRes) setPoolFresh(false); }, [restRes]);

  async function reload() {
    // Handled by bumping the key the hooks watch.
    setReloadKey(k => k + 1);
  }

  async function add(e) {
    e.preventDefault();
    setSaving(true); setErr('');
    try {
      await apiFetch('/lcu/units', {method:'POST', token, body:{serial, note}});
      setSerial(''); setNote(''); setAdding(false);
      setToast(t.lcuAdded);
      // The new reader is today's outstanding test, so this reload is the one the
      // gate refuses -- which opens the prompt through the hook's error handler.
      reload();
    } catch(ex) { setErr(ex.message); }
    finally { setSaving(false); }
  }

  async function answer(unitID, result) {
    await apiFetch('/lcu/tests', {method:'POST', token, body:{results:[{unit_id:unitID, result}]}});
    setToast(t.lcuRecorded);
    reload();
  }

  async function remove(id) {
    try {
      await apiFetch(`/lcu/units/${id}`, {method:'DELETE', token});
      setToast(t.lcuDeleted);
      reload();
    } catch(ex) { setErr(ex.message); }
  }

  /* Removing from the resting history.

     It RE-THROWS rather than reporting, because the two callers want different
     things: an active card wants the message on the page banner, and a resting row
     wants it next to the button. The row catches a 409 and keeps it to itself; the
     active card lets it land on the banner as before. */
  async function removeResting(unit) {
    setRestBusy(true);
    try {
      await apiFetch(`/lcu/units/${unit.id}`, {method:'DELETE', token});
      setToast(t.lcuDeleted);
      reload();
    } finally {
      setRestBusy(false);
    }
  }

  return (
    <div style={{maxWidth:900}}>
      <div className="lcu-howto" style={{marginBottom:18}}>
        {Ic.info}
        <span>{withDays(t.lcuIntro)}</span>
      </div>

      {/* THE POOL, ABOVE EVERYTHING ELSE.

          The number frames the work, and the "+1" is an event about a count -- so the
          news has to land where people are already looking. My first candidate put
          this block below the resting history, which is where the credit is most
          legible; that is wrong, because by then you have scrolled past the thing the
          toast just told you changed.

          The block is CONDITIONAL on a non-zero pool rather than rendering a zero. A
          large grey "0" above an "Add test unit" button reads as a shortfall, when the
          honest statement is that nobody has run a trial yet -- which is silence, and
          silence is what an unused feature looks like. */}
      {(pool > 0 || credits.length > 0) && (
        <div className="lcu-pool-grid">
          <div className="stat-card">
            <div className="stat-label">{t.lcuPool}</div>
            <div className={'stat-value' + (poolFresh ? ' lcu-pool-fresh' : '')}>{pool}</div>
            <div className="stat-sub">{t.lcuPoolSub}</div>
          </div>
          {/* OMITTED ENTIRELY at zero rather than shown as "0". A zero here is a
              statement about the reader ("I have done nothing"); the true statement is
              "none of these are mine", which is not worth a tile. */}
          {yours > 0 && (
            <div className="stat-card">
              <div className="stat-label">{t.lcuPoolYours}</div>
              <div className="stat-value">{yours}</div>
            </div>
          )}
        </div>
      )}

      <div className="team-viewbar" style={{marginBottom:16}}>
        <button className="btn btn-primary" onClick={()=>setAdding(a=>!a)}>
          {Ic.plus} {t.lcuAddUnit}
        </button>
      </div>

      {adding && (
        <form onSubmit={add} className="admin-form" style={{marginBottom:20}}>
          <div className="form-group">
            <label htmlFor="lcu-serial">{t.lcuSerial}</label>
            <input id="lcu-serial" className="form-control" value={serial} maxLength={64} required autoFocus
              placeholder={t.lcuSerialPlaceholder} onChange={e=>setSerial(e.target.value)}/>
          </div>
          <div className="form-group">
            <label htmlFor="lcu-note">{t.lcuNote}</label>
            <input id="lcu-note" className="form-control" value={note} maxLength={200}
              placeholder={t.lcuNotePlaceholder} onChange={e=>setNote(e.target.value)}/>
          </div>
          <div style={{display:'flex',gap:8}}>
            <button type="button" className="btn btn-ghost" onClick={()=>setAdding(false)} disabled={saving}>
              {t.cancel}
            </button>
            <button type="submit" className="btn btn-primary" disabled={saving || !serial.trim()}>
              {saving ? '…' : withDays(t.lcuStartTrial)}
            </button>
          </div>
        </form>
      )}

      {err && <div className="auth-error" style={{marginBottom:16}}>{err}</div>}

      {/* Gated on the error as well as the rows: a failed fetch also leaves rows
          null, so asking only for rows printed "Loading analytics…" underneath the
          red line above, which says the request already failed. */}
      {!paged.rows && !paged.error && <div className="table-card"><div className="empty-state"><p>{t.analyticsLoading}</p></div></div>}

      {paged.rows && units.length === 0 && (
        <div className="table-card">
          <div className="empty-state">{Ic.server}<p>{t.lcuNone}</p></div>
        </div>
      )}

      {paged.rows && units.map((u, i)=>(
        <LCUUnitCard key={u.id} unit={u} today={today} t={t} index={i}
          onAnswer={answer} onDelete={remove}/>
      ))}

      <LoadMore list={paged} t={t}/>

      {/* THE RESTING HISTORY, below the active work rather than beside it.

          A first run therefore shows ONE empty state, not two: the whole section --
          header, table, LoadMore and all -- is withheld until there is something in
          it. Two stacked .empty-state blocks two hundred pixels apart, on a page whose
          job is "add a reader", is a lot of nothing.

          Gated on !rows && !error rather than on rows alone, for the reason the active
          list's loading state gives at the top of this file: rows is null both while a
          request is in flight and after one failed, so asking only for rows prints
          "Loading analytics..." underneath a red line saying the request already
          failed. */}
      {(restRows.length > 0 || pool > 0) && (
        <div className="table-card" style={{marginTop:4}}>
          <div className="table-header">
            <span className="table-title">{t.lcuResting}</span>
            <span className="lcu-meta">{t.lcuRestingSub}</span>
          </div>

          {!restingList.rows && !restingList.error && (
            <div className="empty-state"><p>{t.analyticsLoading}</p></div>
          )}

          {restingList.rows && restRows.length === 0 && (
            <div className="empty-state">{Ic.server}<p>{t.lcuRestingNone}</p></div>
          )}

          {restRows.length > 0 && (
            <table className="lcu-resting-table">
              <thead>
                <tr>
                  <th>{t.lcuSerial}</th>
                  <th>{t.lcuColVerdict}</th>
                  <th>{t.lcuColWindow}</th>
                  <th>{t.lcuColResult}</th>
                  <th>{t.lcuColResolved}</th>
                  <th>{t.lcuPool}</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {restRows.map(u => (
                  <LCURestingRow key={u.id}
                    t={t} lang={lang} unit={u} busy={restBusy}
                    // Only the rows THIS sweep credited animate. Animating all forty on
                    // first paint would be noise, and it looks like a loading bug.
                    fresh={!!u.pool_counted_at
                      && credits.some(c => c.unit_id === u.id)}
                    onDelete={removeResting}/>
                ))}
              </tbody>
            </table>
          )}

          <LoadMore list={restingList} t={t}/>
        </div>
      )}
    </div>
  );
}

/*
  The daily LCU prompt. This has no close button on purpose: a reader being
  trialled is only judged on seven honest days, so a result has to be given
  before the app is usable. The server refuses everything else until it is, so
  this dialog is the way through rather than a decoration over a locked app --
  the only way out of it is answering or logging out.
*/
function LCUPrompt({ units, token, onDone, onLogout }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  // One entry per unit, all of which must be answered before the form will send.
  const [answers, setAnswers] = useState(() => Object.fromEntries(units.map(u=>[u.id, ''])));
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  // No Escape here, unlike every other dialog: this one has no close button on
  // purpose, because the only ways out are answering or signing out and a
  // keyboard user should not get a third. Focus is still moved in and still
  // trapped, which is the part that is about the page behind rather than about
  // dismissing this.
  const panelRef = useDialog({preventClose: true});

  const complete = units.every(u => answers[u.id]);

  async function submit() {
    setSaving(true); setErr('');
    try {
      await apiFetch('/lcu/tests', {method:'POST', token, body:{
        results: units.map(u=>({unit_id:u.id, result:answers[u.id]})),
      }});
      onDone();
    } catch(ex) {
      setErr(ex.message);
      setSaving(false);
    }
  }

  return (
    <div className="modal-overlay" style={{zIndex:200}}>
      <div className="modal" style={{maxWidth:480}} ref={panelRef} role="dialog" aria-modal="true" aria-label={t.lcuGateTitle}>
        <div className="modal-header">
          <h2>{t.lcuGateTitle}</h2>
        </div>
        <div className="modal-body lcu-gate-body">
          <p style={{margin:0,fontSize:13,color:'var(--gray-600)'}}>{t.lcuGateBody}</p>

          <div className="lcu-howto">
            {Ic.info}
            <span>{t.lcuHowToTest}</span>
          </div>

          {units.map(u=>(
            <div key={u.id} className="lcu-gate-unit">
              <div style={{display:'flex',alignItems:'center',justifyContent:'space-between',marginBottom:9}}>
                <span className="lcu-serial">{u.serial}</span>
                {u.note && <span className="lcu-meta">{u.note}</span>}
              </div>
              <div className="lcu-choice">
                <button className={`lcu-choice-btn pass ${answers[u.id]==='pass'?'on':''}`}
                  onClick={()=>setAnswers(a=>({...a,[u.id]:'pass'}))} disabled={saving}>
                  <span className="lcu-bulb"/>{t.lcuPassed}
                </button>
                <button className={`lcu-choice-btn fail ${answers[u.id]==='fail'?'on':''}`}
                  onClick={()=>setAnswers(a=>({...a,[u.id]:'fail'}))} disabled={saving}>
                  <span className="lcu-bulb"/>{t.lcuFailed}
                </button>
              </div>
            </div>
          ))}

          {err && <div className="auth-error">{err}</div>}
        </div>
        <div className="modal-footer" style={{flexDirection:'column',alignItems:'stretch',gap:8}}>
          <button className="btn btn-primary btn-full" disabled={!complete || saving} onClick={submit}>
            {saving ? t.analyticsLoading : t.lcuRecordToday}
          </button>
          <div className="lcu-lockout">{t.lcuGateLocked}</div>
          <button className="btn btn-ghost btn-full" onClick={onLogout} disabled={saving}>
            {t.lcuGateLogOut}
          </button>
        </div>
      </div>
    </div>
  );
}


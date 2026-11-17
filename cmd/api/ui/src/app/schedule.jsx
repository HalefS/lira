function SchedulePage({ token, setToast, onGoToSettings }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];

  const [weekISO, setWeekISO] = useState(() => mondayISO(new Date()));
  const [data, setData]     = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError]   = useState('');
  /* A refusal from a save, kept apart from `error` on purpose. `error` is the
     page-level banner for a page that did not load, which is a different thing from
     a save that was refused; putting a week-closed refusal in the page banner would
     make a working page look broken. */
  const [saveErr, setSaveErr] = useState('');
  const [mode, setMode]     = useState('view');
  const [draft, setDraft]   = useState(null);
  const [busy, setBusy]     = useState(false);
  const [announce, setAnnounce] = useState('');
  const [confirmDiscard, setConfirmDiscard] = useState(false);
// Absences are written IMMEDIATELY, the way a retire is written in the Settings
  // card, rather than staged in the rota draft.
  //
  // This is the one place the two features deliberately do NOT share a save. An
  // absence is not a rota cell: it is dated, it outlives any week on screen, and it
  // belongs to somebody's calendar rather than to the standing pattern. Putting it in
  // the draft would mean a manager could lose a colleague's holiday by pressing Cancel
  // because they changed their mind about a shift three cells away -- and the save bar
  // counts PEOPLE with rota edits, so the absence would not even appear in "1 unsaved".
  //
  // The trade is one honest wrinkle, and it is better than the alternative: after
  // marking somebody away, Save is disabled because nothing in the DRAFT changed. That
  // is correct -- the absence was already saved, and the toast said so a moment
  // earlier -- and the answer is the toast, not a save bar that pretends otherwise.
  const [absDlg, setAbsDlg] = useState(null);        // the open AbsenceDialog
  const [absTarget, setAbsTarget] = useState(null);  // the open remove confirmation
  const [absBusy, setAbsBusy] = useState(false);
  const [absErr, setAbsErr] = useState('');

  // The attendance-sheet download. pdfUnavailable is SEPARATE from pdfError on
  // purpose: a failed generation is worth retrying, while a 501 means this server
  // has no browser at all and will still have none in ten minutes. Retrying that
  // burns a request and a 30s ceiling for nothing, so the button goes dead and the
  // notice explains why.
  const [pdfBusy, setPdfBusy] = useState(false);
  const [pdfError, setPdfError] = useState('');
  const [pdfUnavailable, setPdfUnavailable] = useState(false);

  /* Download the weekly attendance sheet.
     Manager-only endpoint. Fetches with the bearer header rather than following a
     plain link, which is the same reason the consumables download is written this
     way. Two rules about the response worth stating:

       - The server names its own filename in Content-Disposition and the browser
         honours it on a blob download, so the date range in the name is the
         server's, not a second calendar computed here.
       - 501 is mapped to a message for the person who pressed the button, never
         to the server's raw wording, which names a chromium binary for an
         operator. The raw body goes to the console instead. */
  /* Download the attendance sheet. Both formats come from the same view of the
     same rota, so there is nothing to keep in step between them -- and the
     spreadsheet needs no browser on the server, which is why it is offered at all
     rather than being left as a server-side script.

     One busy flag for both: two downloads racing would mean two Chrome runs and
     two workbook fills at once, for a button nobody asked to double-click.

     The server names its own filename in Content-Disposition and the browser
     honours it on a blob download, so the date range in the name is the server
     fact and not a second calendar computed here. */
  async function downloadAttendanceSheet(ext) {
    if (pdfBusy || pdfUnavailable) return;
    setPdfBusy(true);
    setPdfError('');
    try {
      const res = await fetch(`${API}/reports/rota/weekly.${ext}?week=${weekISO}`, {
        headers: { 'Authorization': `Bearer ${token}` },
      });
      if (!res.ok) {
        let msg = t.rotaDownloadPdfTitle;
        try {
          const body = await res.json();
          if (body.error) {
            msg = typeof body.error === 'string'
              ? body.error : Object.values(body.error).join(', ');
          }
        } catch {}
        if (res.status === 501) {
          setPdfUnavailable(true);
          setPdfError(t.rotaDownloadPdfUnavailable);
          setToast(t.rotaDownloadPdfUnavailable);
          console.warn('attendance sheet unavailable:', msg);
        } else {
          setPdfError(msg);
          setToast(msg);
        }
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = '';
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (e) {
      setPdfError(String(e.message || e));
      setToast(String(e.message || e));
    } finally {
      setPdfBusy(false);
    }
  }


  // From the cell picker: "mark this person away on this date", with the kind the
  // manager already chose. The date comes from the cell rather than from today,
  // because the whole point is that a manager can be looking at next week.
  function openAbsAdd(userId, name, kind, dateISO) {
    setAbsErr('');
    setAbsDlg({
      id: null, userId, name, kind: kind || 'vacation',
      starts_on: dateISO, ends_on: dateISO, note: '',
    });
  }

  // From the cell picker on an absent cell. The note is NOT in the grid payload --
  // AbsenceRef has no field for it -- so it is fetched here, once, when a manager
  // actually opens the dialog. That is the price of the type boundary that keeps it
  // off a public noticeboard, and it is a price worth paying on a rare action.
  async function openAbsEdit(a, name) {
    setAbsErr('');
    setAbsBusy(true);
    try {
      const res = await apiFetch('/absences/' + a.id, { token });
      const full = res.absence || a;
      setAbsDlg({
        id: full.id, userId: full.user_id, name,
        kind: full.kind, starts_on: full.starts_on, ends_on: full.ends_on,
        note: full.reason || '', version: full.version,
      });
    } catch (ex) {
      setAbsErr((ex && ex.message) || 'Request failed');
      setToast(t.rotaAbsLoadFailed);
    } finally {
      setAbsBusy(false);
    }
  }

  function openAbsRemove(dlg) {
    setAbsTarget({ id: dlg.id, name: dlg.name, starts_on: dlg.starts_on, ends_on: dlg.ends_on });
  }

  async function saveAbs() {
    if (!absDlg) return;
    setAbsBusy(true);
    setAbsErr('');
    const body = {
      user_id: absDlg.userId, kind: absDlg.kind,
      starts_on: absDlg.starts_on, ends_on: absDlg.ends_on,
      reason: absDlg.note || '',
    };
    if (absDlg.id) body.version = absDlg.version;
    try {
      await apiFetch(absDlg.id ? '/absences/' + absDlg.id : '/absences', {
        method: absDlg.id ? 'PATCH' : 'POST', token, body });
      setAbsDlg(null);
      // Re-read the grid rather than patching the absence into local state: the
      // window filter, the continuation marker and the per-member hours all derive
      // from the range, and re-deriving them here would be a second implementation of
      // rules the server already owns.
      await load(weekISO);
      setToast(absDlg.id ? t.rotaAbsSaved : t.rotaAbsAdded);
    } catch (ex) {
      setAbsErr((ex && ex.message) || 'Request failed');
    } finally {
      setAbsBusy(false);
    }
  }

  async function removeAbs() {
    if (!absTarget) return;
    setAbsBusy(true);
    setAbsErr('');
    try {
      await apiFetch('/absences/' + absTarget.id, { method: 'DELETE', token });
      setAbsTarget(null);
      setAbsDlg(null);
      await load(weekISO);
      setToast(t.rotaAbsDeleted);
    } catch (ex) {
      setAbsErr((ex && ex.message) || 'Request failed');
    } finally {
      setAbsBusy(false);
    }
  }

  const onThisWeek = weekISO === mondayISO(new Date());
  const canEdit = !!(data && data.canEdit);
  /* Two INDEPENDENT axes, and collapsing them is how a technician ends up looking
     like a manager on a closed week.

       canEdit   may this person change a rota at all? (a role question)
       editable  may THIS WEEK be changed?           (a clock question)

     Both must hold to edit, and they fail for different reasons, so the page says
     different things for each: rotaReadOnly for a manager on an open week, and the
     closed-week chip for both roles on a week that is over. A week the server has
     not reported on yet is treated as open -- see load(). */
  const editable = !!(data && data.editable);
  const weekStatus = (data && data.weekStatus) || 'current';
  const recorded = !!(data && data.recorded);
  const historyExact = !!(data && data.historyExact);
  const isPast = weekStatus === 'past';
  const editing = mode === 'edit' && canEdit && editable;

  const load = useCallback(wk => {
    setLoading(true);
    return apiFetch('/schedule?week=' + wk, {token})
      .then(res => {
        setData({
          week:     res.week,
          members:  res.members || [],
          shifts:   res.shifts  || [],
          // Dated ranges, already filtered by the server to the week on screen and
          // already reduced by it for anybody who is not a manager. Both of those are
          // the server's job and neither is re-done here.
          absences: res.absences || [],
          canEdit:  !!res.can_edit,
          // The four week-level facts, straight from the server.
          //
          // None of them can be derived here, and two of them are actively dangerous
          // to guess. "Is this week over" is a question about the CLOCK, and a client
          // that answered it from its own clock would disagree with the server by up
          // to a day across a timezone boundary -- which is the difference between a
          // rota you may still fix and one that is a record. And `editable` is the
          // server's authority on whether a write will be refused; the client only
          // uses it to stop offering the control, never to decide that a write is
          // allowed.
          //
          // `weekStatus` and `recorded` default to 'current'/false rather than to
          // null so that a response from an older server cannot produce a week that
          // claims to be closed. Failing OPEN here is deliberate: the worst case is an
          // edit the server refuses with 422, which is handled below. Failing closed
          // would freeze every rota until the server was upgraded.
          weekStatus:   res.week_status   || 'current',
          editable:     res.editable     !== false,
          recorded:     !!res.recorded,
          historyExact: res.history_exact !== false,
        });
        setError('');
      })
      .catch(ex => setError((ex && ex.message) || 'Request failed'))
      .finally(() => setLoading(false));
  }, [token]);

  useEffect(() => { load(weekISO); }, [weekISO, load]);

  /* The legend is built from what the cells actually reference, not from the
     catalog alone. A shift that has been retired but is still on somebody's week
     comes back inside that day's own object and nowhere else, so a legend built
     from /v1/shifts alone would print a name into the grid that the reader then
     cannot look up. Sorted earliest start first: a rota reads forward in time. */
  const legend = useMemo(() => {
    const seen = new Map();
    for (const x of (data ? data.shifts : [])) seen.set(x.id, x);
    for (const m of (data ? data.members : []))
      for (const d of (m.days || [])) if (d && d.id != null && !seen.has(d.id)) seen.set(d.id, d);
    return [...seen.values()].sort((a, b) =>
      String(a.start_time).localeCompare(String(b.start_time)) || a.name.localeCompare(b.name));
  }, [data]);

  /* Dirty is the set of MEMBERS whose days differ, not the set of cells. One PUT
     goes out per person, so a count of cells would overstate the work and a count
     of changes would understate it. */
// A person's name, for a confirmation that has to say who it is about.
  // ConfirmDeleteIssue repeats its subject for the same reason, and here the subject
  // is a colleague rather than a record.
  // Whether this installation has ANY accounts, which is not the same question as
  // whether anybody is on the rota and cannot be answered from the grid: an empty grid
  // means the roster is empty, and says nothing about whether there is anybody to put
  // on it. GET /v1/rota/members is requireAuth, so this is unknowable to an anonymous
  // visitor -- and they are not shown a failed request as though it were an answer.
  //
  // Fetched ONLY when the board is already empty AND the caller can edit, so the
  // ordinary grid path issues no extra request and an anonymous visitor gets none.
  // `anyAccounts` is tri-state: null means "not known", which is what keeps the
  // no-accounts message off the public page entirely.
  const [anyAccounts, setAnyAccounts] = useState(null);
  useEffect(() => {
    if (!canEdit || !data || (data.members || []).length > 0) return;
    let live = true;
    apiFetch('/rota/members', { token })
      .then(res => { if (live) setAnyAccounts((res.members || []).length > 0); })
      // A failure here leaves it null, i.e. unknown, and the page then shows the
      // generic empty-roster message. Guessing "no accounts" from a failed request would
      // be the same class of bug as the absence-kind redaction getting backwards.
      .catch(() => { if (live) setAnyAccounts(null); });
    return () => { live = false; };
  }, [canEdit, data, token]);

  const nameFor = userId => {
    const m = (data && data.members || []).find(x => x.user_id === userId);
    return m ? m.name : '';
  };

  // The absence half of the legend: one chip per KIND, counting PEOPLE, built from
  // what is actually on screen rather than from anything the client already knows.
  //
  // Deduplicating by kind is what makes the redaction work from this side too: the
  // payload already says "unavailable" where a manager would see "sick leave", so a
  // public board shows "Unavailable - 2 people" and never mentions sick leave. And
  // that follows from the payload rather than from a second client-side remap that
  // could disagree with it.
  //
  // Counting PEOPLE rather than rows, and counting each person once per kind they are
  // actually shown under.
  //
  // Both halves matter. Rows would say a fortnight of holiday is two people away. And
  // a plain per-kind count would say "Holiday - 1 person, Sick leave - 1 person" about
  // ONE person who is away all week and happens to have sick leave recorded inside
  // their holiday -- two chips reading as two people, which is the number a manager
  // staffing a shift would actually use.
  //
  // So it walks the grid's own rule: for each member and each day, the absence the
  // CELL shows, which is the first one covering it. A person appears under a kind only
  // where they are genuinely shown as that kind, and a sick absence inside a holiday
  // contributes to the holiday -- which is also the more truthful statement, since the
  // holiday is why they are away.
  const absLegend = useMemo(() => {
    if (!data) return [];
    const byUser = absencesByUser(data.absences);
    const counts = new Map();
    for (const m of (data.members || [])) {
      const list = byUser.get(m.user_id);
      if (!list) continue;
      const seen = new Set();
      for (const d of data.week.days) {
        const a = absenceAt(list, d.date);
        if (a) seen.add(a.kind);
      }
      for (const kind of seen) counts.set(kind, (counts.get(kind) || 0) + 1);
    }
    return [...counts.entries()].map(([kind, count]) => ({ kind, count }));
  }, [data]);

  const dirtyIds = useMemo(() => {
    if (!editing || !draft || !data) return [];
    const was = new Map(data.members.map(m => [m.user_id, m]));
    return draft
      .filter(m => {
        const before = was.get(m.user_id);
        if (!before) return false;
        return (m.days || []).some((d, i) => {
          const now = (d || {}).id != null ? d.id : null;
          const wasId = ((before.days || [])[i] || {}).id != null ? before.days[i].id : null;
          return now !== wasId;
        });
      })
      .map(m => m.user_id);
  }, [editing, draft, data]);

  function enterEdit() {
    // A fresh days array per member and whole-day replacement on every write, so
    // nothing in the draft can reach back into data.members.
    setDraft((data.members || []).map(m => ({ ...m, days: (m.days || []).slice() })));
    setMode('edit');
  }
  function leaveEdit() { setMode('view'); setDraft(null); }
  function cancelEdit() {
    if (dirtyIds.length > 0) { setConfirmDiscard(true); return; }
    leaveEdit();
  }

  function setCell(userID, index, shift) {
    setDraft(prev => prev.map(m => {
      if (m.user_id !== userID) return m;
      const days = (m.days || []).slice();
      days[index] = shift;
      return { ...m, days };
    }));
  }

  async function save() {
    setBusy(true);
    setSaveErr('');
    try {
      // Sequential, not Promise.all: a burst of seven PUTs that half-succeeds
      // leaves a rota nobody can reason about, and this keeps one failure to one
      // member with the rows before it already saved and honest.
      for (const userID of dirtyIds) {
        const m = draft.find(x => x.user_id === userID);
        const res = await apiFetch('/schedule/' + userID + '?week=' + weekISO, {
          method: 'PUT',
          token,
          // ALL seven, always. Omitted days are cleared server-side, so a payload
          // built from only the touched cells would wipe the rest of the week.
          // This is the single most important line on the page.
          //
          // And ?week= names the week on SCREEN, not the week containing today.
          // Without it the write would land on the current week, so a manager
          // tidying next Tuesday would quietly rewrite this Tuesday -- which is the
          // exact bug the per-week rota was built to remove, reappearing one layer up
          // in the client.
          body: { days: (m.days || []).map((d, i) => ({ weekday: i + 1, shift_id: d ? d.id : null })) },
        });
        // Replaced wholesale from the echoed member, so the row on screen is
        // exactly what a fresh load would show -- including a server-normalised
        // overnight flag this client did not predict.
        setData(d => ({
          ...d,
          members: d.members.map(x => (x.user_id === userID ? res.member : x)),
        }));
      }
      /* Saving a week that had never been written to is the moment it becomes its
         own schedule, so the "inherited" note is no longer true. Clearing it here
         rather than waiting for the reload keeps the page from contradicting itself
         in the second between the save and the fetch. */
      if (!recorded) {
        setData(d => (d ? { ...d, recorded: true, weekStatus: d.weekStatus } : d));
      }
      leaveEdit();
      setToast(t.rotaSaved);
      setAnnounce(t.rotaSaved);
    } catch (ex) {
      /* 422 week_closed is not a failure of the request; it is the server telling us
         the week ended while the page was open. That is worth saying in the page's
         own words and not as "Error: ...", because the manager did nothing wrong and
         a red error toast says they did.

         It is matched on the `reason` field and never on the English sentence: a
         message that changes when somebody rewords it is a message this cannot
         safely match on. Anything else keeps the old generic path. */
      if (ex && ex.reason === 'week_closed') {
        setSaveErr(t.rotaWeekClosedToast);
        setToast(t.rotaWeekClosedToast);
        setAnnounce(t.rotaWeekClosedToast);
      } else {
        setToast('Error: ' + ((ex && ex.message) || 'Request failed'));
      }
      // Out of edit mode and back to the server's truth. A draft that cannot be
      // written is not something to leave on screen looking editable.
      leaveEdit();
      load(weekISO);
    } finally {
      setBusy(false);
    }
  }

  /* Announced once per mode change. The 30ms gap is load-bearing: a live region
     whose text is set to the identical string is not a change, so leaving edit
     mode and coming straight back would announce nothing at all. */
  useEffect(() => {
    if (!editing) { setAnnounce(''); return; }
    setAnnounce('');
    const id = setTimeout(() => setAnnounce(t.rotaEnteredEdit), 30);
    return () => clearTimeout(id);
  }, [editing, t]);

  // Stepping the week while editing reloads, which would throw the draft away
  // without a word -- so it asks first, exactly as Cancel does.
  function stepWeek(n) {
    if (editing && dirtyIds.length > 0) { setConfirmDiscard(true); return; }
    setWeekISO(w => shiftISO(w, n));
  }
  function goThisWeek() {
    if (editing && dirtyIds.length > 0) { setConfirmDiscard(true); return; }
    setWeekISO(mondayISO(new Date()));
  }

  if (error) {
    return (
      <div style={{maxWidth:1160}}>
        <div className="table-card">
          <div className="empty-state">
            {Ic.info}
            <p className="rota-empty-head">{t.rotaLoadFailed}</p>
            <p className="rota-empty-sub">{error}</p>
            <button type="button" className="btn btn-ghost" onClick={() => load(weekISO)}>
              {Ic.refresh} {t.tryAgain}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div style={{maxWidth:1160}}>
        <div className="table-card">
          <div className="empty-state">{Ic.calendar}<p>{t.analyticsLoading}</p></div>
        </div>
      </div>
    );
  }

  const range = data.week.start + ' \u2013 ' + data.week.end;

  return (
    <div style={{maxWidth:1160}}>
      {/* One polite live region for the whole page. It carries the two things a
          change of mode or a save has to say, and it stays mounted whether or not
          there is anything to say -- an empty region is not announced, so there
          is nothing to mount and unmount around it. */}
      <div className="sr-only" role="status" aria-live="polite">{announce}</div>

      <div className="table-card">
        <div className="table-header">
          <div>
            <div className="table-title">{t.rotaTitle}</div>
            <div style={{fontSize:12,color:'var(--gray-400)',marginTop:2}}>
              {/* "0 people, 4 shifts" is TRUE and useless, so an empty board gets its
                  own clause rather than a pair of numbers. The count itself needs no
                  change: it moves on its own, because data.members is now the filtered
                  set. A member on the rota with no assignments still counts -- they
                  have to, or the subtitle disagrees with the number of rows below it. */}
              {(data.members || []).length === 0 && legend.length > 0
                ? t.rotaSubtitleEmpty
                : t.rotaSubtitle
                    .replace('{members}', (data.members || []).length)
                    .replace('{shifts}', legend.length)}
            </div>
          </div>

          <div className="table-actions">
            {/* WHO is on the rota, which lives in Settings. Here for managers only,
                and the gate is can_edit rather than a session check for the reason
                rotaReadOnly gives: the server reports can_edit:false for a technician
                and for an anonymous visitor alike, and drawing this for either would be
                a guess about somebody's permissions that the server would then refuse.

                It sits with the week controls rather than near the grid because it
                NAVIGATES rather than acting on what is below it -- and because a
                control that changed the grid it sat above, on a page anonymous visitors
                are reading at the same time, invites the "did I just delete Marco?"
                panic. One link, one direction; cat-members carries the way back.

                No count here. The number of people on the rota is already the
                subtitle directly above, and saying it twice invites the reader to
                wonder which one is current. */}
            {canEdit && onGoToSettings && (
              <button type="button" className="rota-rosterlink"
                onClick={() => onGoToSettings('cat-members')}>
                {Ic.users} {t.rotaManageRoster}
              </button>
            )}

            {/* Week navigation. Two arrows and a range: nothing that looks like a
                place you can go and stay, because a standing rota does not have
                weeks to go to. */}
            <div className="rota-week" role="group" aria-label={t.rotaWeekNav}>
              <button type="button" className="rota-week-btn rota-week-prev"
                onClick={() => stepWeek(-7)} disabled={loading || busy}
                title={t.rotaPrevWeek} aria-label={t.rotaPrevWeek}>
                {Ic.chevron}
              </button>
              <span className="rota-week-range">{range}</span>
              <button type="button" className="rota-week-btn"
                onClick={() => stepWeek(7)} disabled={loading || busy}
                title={t.rotaNextWeek} aria-label={t.rotaNextWeek}>
                {Ic.chevron}
              </button>
              {/* Already here: a fact, not a control. Somewhere else: the same
                  slot becomes the way back. */}
              {onThisWeek
                ? <span className="rota-week-pill">{t.rotaThisWeek}</span>
                : <button type="button" className="rota-week-pill" onClick={goThisWeek}>
                    {t.rotaThisWeek}
                  </button>}
              {/* WHERE THIS WEEK IS, as a fact rather than a control.

                  It sits inside the week navigation because it qualifies exactly
                  what those arrows are about to reach: the arrows say "another
                  week", this says "and that one is over".

                  Past gets its own word, because a closed week is not a lesser
                  kind of current one -- it is a record, and the manager is about
                  to be told they cannot change it. A future week that is still
                  INHERITED gets the quieter marker instead, which says what is
                  true without stopping anybody.

                  Not a third case for the current week: this week is the default,
                  and a chip on every page would be furniture. */}
              {isPast
                ? <span className="rota-weekstate is-past">{t.rotaWeekPast}</span>
                : (weekStatus === 'future' && !recorded
                    ? <span className="rota-weekstate">{t.rotaWeekInherited}</span>
                    : null)}
            </div>

            {/* A segmented radiogroup, not a toggle whose label morphs to "Done":
                "Done" is ambiguous the moment there are unsaved changes, and a
                morphing label takes the focused element off the screen.

                Gated on canEdit AND editable. A past week hides the switch
                altogether rather than showing a disabled Edit button, because a
                disabled control invites "why?" and the answer -- this week is a
                record of what people worked -- deserves a sentence rather than a
                greyed-out button. The chip below says that sentence instead. */}
            {canEdit && editable ? (
              <>
                <div className="rota-modeswitch" role="radiogroup" aria-label={t.rotaModeSwitch}>
                  <button type="button" role="radio" className="rota-mode-btn"
                    aria-checked={mode === 'view'} onClick={leaveEdit}
                    aria-label={t.rotaView}>
                    {Ic.eye}<span>{t.rotaView}</span>
                  </button>
                  <button type="button" role="radio" className="rota-mode-btn"
                    aria-checked={editing} onClick={enterEdit} disabled={loading}
                    aria-label={t.rotaEdit}>
                    {Ic.edit}<span>{t.rotaEdit}</span>
                  </button>
                </div>
                {editing && dirtyIds.length > 0 && (
                  <span className="rota-dirty">
                    {t.rotaUnsaved.replace('{count}', dirtyIds.length)}
                  </span>
                )}
              </>
            ) : (
              /* One neutral chip, and nothing else changes: same grid, same
                 legend, same week navigation, no disabled controls and no lock.
                 Its only job is to answer "is this page broken?" before anyone
                 has to ask -- an anonymous visitor lands on a public endpoint,
                 finds no Edit button, and without this the absence is
                 indistinguishable from a failed load.

                 The SAME chip serves a manager looking at a closed week, and it
                 says something different, because the two situations look identical
                 otherwise and have nothing in common: one is "you may not, ever"
                 and the other is "you may not, this week is over". A visitor who
                 was told "read only" would think the rota was broken; a manager
                 who was told the visitor's sentence would think their own
                 permissions had gone. */
              <span className="rota-readonly">{Ic.eye}
                <span>{isPast ? t.rotaWeekClosed : t.rotaReadOnly}</span>
              </span>
            )}

            {/* The attendance sheet. Last in the toolbar, and a ghost rather than a
                primary button: the committing action on this page is Save, in the
                save bar below the grid, and two solid blue buttons on one screen
                compete for the same eye.

                disabled while editing is load-bearing, not defensive. The server
                holds the SAVED week and the screen shows the draft, so downloading
                mid-edit prints a sheet that does not match what is on screen --
                the same class of bug as "0 unsaved" appearing after an absence
                save. The tooltip says so rather than leaving a dead control.

                The label does NOT change to t.generatingPdf while it works. A
                morphing label takes the focused element off the screen -- already
                the reason the mode switch is a radiogroup and not a Done toggle --
                and it changes the button width under the pointer mid-click. The
                icon spins instead, and the busy state is announced via aria-busy. */}
            {canEdit && (
              <button type="button" className={'btn btn-ghost rota-pdf-btn'
                  + (pdfBusy ? ' spinning' : '')}
                onClick={() => downloadAttendanceSheet('pdf')}
                disabled={!data || loading || pdfBusy || editing || pdfUnavailable}
                title={editing ? t.rotaPdfLockedWhileEditing
                  : pdfUnavailable ? t.rotaDownloadPdfUnavailable
                  : t.rotaDownloadPdfTitle}
                aria-busy={pdfBusy || undefined}>
                {Ic.download} <span>{t.rotaDownloadPdf}</span>
              </button>
            )}
            {/* The spreadsheet sibling. Ghost like the PDF button and sharing its
                busy state, so neither can be pressed while the other is running.
                It carries no separate "unavailable" state because it needs no
                browser: it works on a server where the PDF cannot be produced at
                all, which is exactly when somebody wants it. */}
            {canEdit && (
              <button type="button" className="btn btn-ghost"
                onClick={() => downloadAttendanceSheet('xlsx')}
                disabled={!data || loading || pdfBusy || pdfUnavailable}
                title={editing ? t.rotaPdfLockedWhileEditing : t.rotaDownloadPdfTitle}
                aria-busy={pdfBusy || undefined}>
                {Ic.download} <span>{t.rotaDownloadXlsx}</span>
              </button>
            )}
          </div>
        </div>

        {/* A failure the manager needs to keep seeing. It does not auto-dismiss,
            because nothing here is going to fix itself: the server either has a
            browser or it does not. */}
        {pdfError && (
          <div className="rota-notice" role="alert">
            {Ic.info}
            <span>{pdfError}</span>
          </div>
        )}

        {/* THE PREMISE, before the grid. Not a hint: it is the one fact that makes
            every cell in the table mean what a reader assumes it means.

            ONE note, never two, chosen by priority. A rota page with a read-only
            banner stacked above an inheritance banner above a history note is a
            page where none of the three gets read, and the one that matters most
            (history_exact) is the easiest to lose that way.

            The order below is the order of how much the reader would be MISLED
            without the sentence, not the order of how interesting it is:

              history_exact false   the app does not remember this week. Before
                                    anything else: a reader who trusts a grid that
                                    was never recorded is trusting a guess.
              past                  a record, and not changeable.
              future, not recorded  shows this week's rota, but is not it.
              current               the standing pattern, and which half is plan.
              otherwise             an ordinary recorded week.

            The last case is the only one with nothing to add. Saying "this week
            has its own schedule" is true of every other week and would be noise. */}
        <div className="rota-note">
          {Ic.info}
          <span>
            {!historyExact ? t.rotaHistoryUnknown
              : isPast ? t.rotaWeekClosedNote
              : (weekStatus === 'future' && !recorded) ? t.rotaWeekInheritedNote
              : onThisWeek ? t.rotaStanding
              : t.rotaOtherWeek}
          </span>
        </div>

        {/* The refusal, from a save that came back 422 week_closed -- the week ended
            while this page was open, which is the only way to reach here. It sits
            inside the save bar rather than the page-level error banner, because it
            is about the thing the manager was doing rather than about the page
            having failed, and it clears itself on the next save. */}
        {saveErr && (
          <div className="rota-savebar-error" role="alert">{saveErr}</div>
        )}

        {(legend.length > 0 || absLegend.length > 0) && (data.members || []).length > 0 && (
          <div className="rota-legend">
            {legend.map(x => (
              <span key={x.id}
                className={'rota-legend-item' + (x.overnight ? ' is-night' : '') + (x.color ? ' is-tinted' : '')}
                style={tintStyle(x)}>
                {x.color && <span className="rota-tint-dot" style={dotStyle(x.color)}/>}
                <span className="rota-legend-name">{x.name}</span>
                <span className="rota-legend-hours">
                  {x.start_time}&ndash;{x.end_time}{x.overnight ? t.rotaPlusOne : ''}
                </span>
                {/* Counted from the grid, not read off assignment_count: one
                    person on Night five times is one person. */}
                <span className="rota-legend-count">
                  {t.rotaPeopleOn.replace('{count}',
                    (data.members || []).filter(m => (m.days || []).some(d => d && d.id === x.id)).length)}
                </span>
                {x.active === false && <span className="rota-legend-retired">{t.rotaRetired}</span>}
              </span>
            ))}
            {/* The absence group, and ONLY when there is one. An empty second group
                behind a rule reads as "absences exist but none are this week", which
                is a different and more alarming statement than "no absences". */}
            {absLegend.length > 0 && <>
              <span className="rota-legend-sep" aria-hidden="true"/>
              {absLegend.map(k => (
                <span key={k.kind} className="rota-legend-abs"
                  style={{ '--abs-ink': absenceHue(k.kind) }}>
                  <span className="rota-legend-abs-ic" aria-hidden="true">
                    {absenceGlyph[k.kind]}
                  </span>
                  <span className="rota-legend-abs-name">{t.rotaAbsKind[k.kind]}</span>
                  <span className="rota-legend-abs-count">
                    {k.count === 1 ? t.rotaAbsPeopleOne : t.rotaAbsPeople.replace('{count}', k.count)}
                  </span>
                </span>
              ))}
            </>}
          </div>
        )}

        {/* No shifts at all: every cell would be a dash, and a page of dashes is
            indistinguishable from "everybody is off all week" -- a materially
            different statement, and the more alarming one to get wrong. So the
            grid does not render. */}
        {legend.length === 0 && absLegend.length === 0 ? (
          <div className="empty-state">
            {Ic.calendar}
            <p className="rota-empty-head">{t.rotaNoShiftsHead}</p>
            <p className="rota-empty-sub">{t.rotaNoShiftsBody}</p>
            {canEdit && onGoToSettings && (
              <button type="button" className="btn btn-primary rota-empty-cta"
                onClick={() => onGoToSettings('cat-shifts')}>
                {Ic.plus} {t.rotaDefineShifts}
              </button>
            )}
          </div>
        ) : (data.members || []).length === 0 ? (
          /* Two different truths share this branch, and they need different copy.

             NO ACCOUNTS AT ALL is only knowable to a manager, because the roster read
             is requireAuth and a noticeboard must not print "there are no accounts in
             this system" to the public. Its message points at the Team page.

             EVERYBODY IS OFF THE ROTA is the ordinary case: a manager has deliberately
             unticked everyone. Its message points at cat-MEMBERS, not cat-shifts --
             the old copy sent the reader to define shifts, which is advice for a state
             where the shifts are the missing thing, and here they exist. */
          <div className="empty-state">
            {Ic.users}
            {anyAccounts === false ? (
              <>
                <p className="rota-empty-head">{t.rotaNoAccountsHead}</p>
                <p className="rota-empty-sub">{t.rotaNoAccountsBody}</p>
              </>
            ) : (
              <>
                <p className="rota-empty-head">{t.rotaNobodyOnRotaHead}</p>
                <p className="rota-empty-sub">{t.rotaNobodyOnRotaBody}</p>
                {canEdit && onGoToSettings && (
                  <button type="button" className="btn btn-primary rota-empty-cta"
                    onClick={() => onGoToSettings('cat-members')}>
                    {Ic.plus} {t.rotaDefineTeam}
                  </button>
                )}
              </>
            )}
          </div>
        ) : (
          <>
            {/* "Nobody is on a shift this week, but people are marked away" is a real
                state, and the no-shifts empty state above is actively misleading in it
                -- it would tell a manager nobody has been rostered when in fact seven
                people are visibly off. The grid still renders so the absences are
                legible; the note says what is actually true about the rota. */}
            {legend.length === 0 && (
              <div className="rota-note">
                {Ic.info}
                <span>
                  {t.rotaAbsOnlyGrid}
                  {canEdit && onGoToSettings && (
                    <button type="button" className="btn btn-ghost rota-empty-cta"
                      onClick={() => onGoToSettings('cat-shifts')}>
                      {Ic.plus} {t.rotaDefineShifts}
                    </button>
                  )}
                </span>
              </div>
            )}
            <RotaTable
              t={t}
              week={data.week}
              members={data.members || []}
              shifts={legend}
              absences={data.absences || []}
              editing={editing}
              draft={draft}
              setCell={setCell}
              onAddAbsence={openAbsAdd}
              onEditAbsence={openAbsEdit}
              onRemoveAbsence={a => openAbsRemove({ ...a, name: nameFor(a.user_id) })}
            />
          </>
        )}

        {/* Save and Cancel, after the grid. .table-footer is already this file's
            home for "a button about the list as a whole rather than an action on
            one row", which is what Save is -- and being after the grid in the DOM
            means Tab out of the last cell lands on Cancel and then Save, with no
            key handling at all. */}
        {editing && (
          <div className="rota-savebar">
            {Ic.info}
            {/* THREE HINTS, ONE SLOT, and the difference between them is the whole
                feature rather than a nicety.

                Editing a week that is not the current one touches that week and
                nothing else, so the hint says so. Editing a FUTURE week is the case
                that used to be a trap -- the write went to the current week and
                redrew it -- and a manager who has just been told "changing this
                affects only this week" is being told something true and worth
                hearing.

                Editing the CURRENT week is the genuinely subtle one, and the hint
                says the rule rather than leaving it to be discovered: days already
                worked are records and stay put, and days still to come are what next
                week repeats. It is one sentence because it is one rule, and it sits
                in the save bar because that is the only place on the page where the
                manager is demonstrably about to act on it.

                Not a modal, and not a warning. A dialog on every cell would be
                unbearable, and a warning that fires when nothing is at stake trains
                people to dismiss it for the thing that matters. */}
            <span>
              {dirtyIds.length === 0
                ? t.rotaEditHintClean
                : onThisWeek ? t.rotaEditHintCurrent
                : weekStatus === 'future' ? t.rotaEditHintFuture
                : t.rotaEditHint}
            </span>
            <span className="rota-savebar-spacer" />
            <button type="button" className="btn btn-ghost" onClick={cancelEdit} disabled={busy}>
              {t.rotaCancel}
            </button>
            <button type="button" className="btn btn-primary" onClick={save}
              disabled={busy || dirtyIds.length === 0}>
              {Ic.check} {busy ? '.' : t.rotaSave}
            </button>
          </div>
        )}
      </div>

      {/* The discard prompt. Only reachable with unsaved edits, and reached from
          three places -- Cancel, the week arrows, and "This week" -- because all
          three would otherwise throw the draft away silently. */}
      {confirmDiscard && <DiscardDialog
        t={t}
        count={dirtyIds.length}
        onCancel={() => setConfirmDiscard(false)}
        onDiscard={() => { setConfirmDiscard(false); leaveEdit(); }}
      />}

      {absDlg && <AbsenceDialog
        t={t}
        dlg={absDlg}
        setDlg={setAbsDlg}
        spanDays={absenceDayCount(absDlg.starts_on, absDlg.ends_on)}
        err={absErr}
        busy={absBusy}
        onCancel={() => { setAbsDlg(null); setAbsErr(''); }}
        onSave={saveAbs}
        onRemove={() => openAbsRemove(absDlg)}
      />}

      {absTarget && <ConfirmRemoveAbsence
        t={t}
        target={absTarget}
        busy={absBusy}
        onCancel={() => setAbsTarget(null)}
        onConfirm={removeAbs}
      />}
    </div>
  );
}

/* The settings page. Named for what it is called in the nav rather than for what
   it grew out of: it holds the catalogs the rest of the app is configured from --
   issue types, departments, Connecta agents, consumable items -- plus the settings
   themselves. The per-section translation keys kept their `admin` prefix, because
   renaming thirty strings nobody sees buys nothing and risks a typo in one of
   them. */
/* Reads what has been typed into the room field and works out how many rooms it
   would add, so the size of a range is visible before it is saved rather than
   after. A range typed carelessly across a floor boundary expands to every number
   in between, and "1399-1402" quietly becoming rooms nobody has is the one way
   this feature can do real damage.

   The rules here mirror data.ParseRoomRun, and the server is the authority: this
   only decides what the preview says, and anything it gets wrong is caught when
   the save is refused. Kept deliberately small rather than sharing code with the
   server, because the alternative is a round trip per keystroke for a preview.
*/

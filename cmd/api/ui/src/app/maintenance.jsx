/* ════════════════ MAINTENANCE PAGE ════════════════ */
/*
  Preventive maintenance on department printers and phones.

  This is a work queue, not a report: what matters is what is due and who is
  going to do it, so the page opens on the overdue list and everything else
  follows in order of urgency.

  The one rule that shapes everything here: recording a check is not the same as
  logging an issue. Almost every check finds nothing, and if each of those became
  an issue the counts and averages everywhere else in the app would quietly
  change. So "Mark done" writes a check, and only a fault offers to also write an
  issue — and that offer can be declined, because a fault put right on the spot
  is a real thing worth recording that is not anybody's problem to track.
*/
const MAINTENANCE_PRESETS = [
  {days:7,    key:'weekly'},
  {days:30,   key:'monthly'},
  {days:90,   key:'quarterly'},
  {days:180,  key:'biannual'},
  {days:365,  key:'yearly'},
];

// The issue type a piece of equipment raises, so a fault logged from here lands
// in the normal tables already categorised.
const maintenanceIssueType = eq => eq === 'phone' ? 'Phone' : 'Printer';

function MaintenanceDeviceCard({ s, t, canManage, onComplete, onHistory, onEdit, index = 0 }) {
  const late = s.due_state === 'overdue';
  const soon = s.due_state === 'due_soon';
  const isPhone = s.equipment_type === 'phone';
  const typeLabel = isPhone ? t.maintenancePhone : t.maintenancePrinter;

  let dueText;
  if (late) {
    dueText = s.days_overdue === 1
      ? t.maintenanceDayOverdue
      : t.maintenanceDaysOverdue.replace('{days}', s.days_overdue);
  } else if (soon) {
    dueText = s.next_due_on === s.last_done_on
      ? t.maintenanceDueToday
      : t.maintenanceNotYet.replace('{date}', formatDateShort(s.next_due_on));
  } else {
    dueText = t.maintenanceNotYet.replace('{date}', formatDateShort(s.next_due_on));
  }

  // The foot answers two questions, and both are dates: when this device was last
  // looked at, and when it is next wanted. next_due_on already arrived with the
  // row, so the countdown is arithmetic on loaded data rather than a new endpoint.
  //
  // "Checked today" rather than today's date, because a date the reader already
  // knows is a date that takes up room to say nothing.
  const todayISO = localISODate(new Date());
  const checkedToday = !!s.last_done_on && String(s.last_done_on).slice(0, 10) === todayISO;
  const dueDay = dayNumber(s.next_due_on);
  const todayDay = dayNumber(todayISO);
  const daysToNextDue = dueDay === null || todayDay === null ? null : dueDay - todayDay;

  let lastText;
  if (!s.last_done_on) {
    lastText = t.maintenanceNeverChecked;
  } else if (checkedToday) {
    lastText = t.maintenanceCheckedToday;
  } else {
    lastText = `${formatDateShort(s.last_done_on)} · ${
      s.last_outcome === 'fault' ? t.maintenanceFault : t.maintenanceAllFine}`;
  }

  // The countdown is only spelled out while the tile is comfortable. An overdue or
  // soon device already says so on the coloured badge above, and a tile that
  // repeats itself is a tile nobody reads.
  let nextText = '';
  if (!late && !soon && s.active && daysToNextDue !== null && daysToNextDue >= 0) {
    nextText = daysToNextDue === 0
      ? t.maintenanceNextToday
      : daysToNextDue === 1
        ? t.maintenanceNextTomorrow
        : t.maintenanceNextIn.replace('{days}', daysToNextDue);
  }

  return (
    <div className={`maint-card maint-enter ${
        late ? 'maint-overdue' : soon ? 'maint-soon' : 'maint-scheduled'
      } ${!s.active ? 'maint-retired' : ''}`}
      // Staggered by position so the queue arrives in order instead of all at
      // once. Capped: a long overdue queue should not take a second to appear.
      style={{animationDelay: `${Math.min(index, 8) * 30}ms`}}>
      <div className="maint-head">
        <span className={`maint-chip maint-chip-${isPhone ? 'phone' : 'printer'}`} aria-hidden="true">
          {isPhone ? Ic.phone : Ic.printer}
        </span>
        <strong className="maint-name">{s.device_name}</strong>
      </div>
      {/* The caption replaces the type pill that used to sit here. The chip beside
          the name already says printer or phone, so a badge saying it again was
          decoration; the words stay as plain text because the chip is
          aria-hidden, and a card whose only statement of what it is lives in a
          picture is unreadable to a screen reader. */}
      <div className="maint-tags">
        <span className="maint-caption">{typeLabel}</span>
        {late && <span className="badge badge-overdue">{dueText}</span>}
        {!late && soon && <span className="badge badge-soon">{dueText}</span>}
        {!s.active && <span className="badge badge-slate">{t.maintenanceRetired}</span>}
      </div>
      {/* One fact per icon, so the line reads without labels and wraps cleanly
          when an assignee makes it longer than the tile. */}
      <div className="maint-sub">
        <span className="maint-fact maint-fact-location">
          <span className="maint-fact-ic" aria-hidden="true">{Ic.pin}</span>{s.department_name}
        </span>
        <span className="maint-fact maint-fact-interval">
          <span className="maint-fact-ic" aria-hidden="true">{Ic.refresh}</span>
          {t.maintenanceIntervalDays.replace('{days}', s.interval_days)}
        </span>
        {/* Shown only when someone owns it. A manager needs to see who, because
            they are the ones who reassign; a technician can only ever see their
            own or a shared one, and "all technicians" on every tile would just
            be noise. */}
        {shortName(s.assignee_name) && <span className="maint-fact">{shortName(s.assignee_name)}</span>}
      </div>
      {/* The foot, under a rule: the two dates a reader scans a queue for. The
          words go green or red with the dot, so a device that was checked today
          and found nothing reads as closed at a glance. */}
      <div className="maint-last">
        <span className={`maint-last-dot ${
          !s.last_done_on ? '' : s.last_outcome === 'fault' ? 'fault' : 'ok'}`}/>
        <span className={`maint-last-when ${
          !s.last_done_on ? '' : s.last_outcome === 'fault' ? 'fault' : 'ok'}`}>
          {lastText}
        </span>
        {nextText && <span className="maint-last-due">{nextText}</span>}
      </div>
      <div className="maint-card-actions">
        {s.active && (
          <button className="btn btn-primary" onClick={()=>onComplete(s)}>
            {/* A device already checked today is not waiting to be done, it has
                been done -- what the button offers is another look. Saying so
                stops the tile reading as though it had been overlooked. */}
            {Ic.check} {checkedToday ? t.maintenanceDoneAgain : t.maintenanceMarkDoneCard}
          </button>
        )}
        <button className="icon-btn" title={t.maintenanceHistory} aria-label={t.maintenanceHistory}
          onClick={()=>onHistory(s)}>{Ic.clock}</button>
        {canManage && (
          <button className="icon-btn" title={t.maintenanceEditDevice} aria-label={t.maintenanceEditDevice}
            onClick={()=>onEdit(s)}>{Ic.edit}</button>
        )}
      </div>
    </div>
  );
}

// formatDateShort renders a YYYY-MM-DD date for display. Parsed as local midnight
// on purpose: new Date('2026-10-03') is UTC midnight, which in Lisbon is still
// the 2nd, and the due date would read a day early for anyone east of Greenwich.
function formatDateShort(iso) {
  if (!iso) return '';
  const [y, m, d] = String(iso).slice(0, 10).split('-').map(Number);
  if (!y || !m || !d) return String(iso);
  return new Date(y, m - 1, d).toLocaleDateString(undefined, {day:'numeric', month:'short'});
}

// reportIssueDate is the day the report attributes an issue to: started_at when
// the work has a start, otherwise created_at. Those are the same two columns
// GetReport uses to place a row in its period, so the date shown can never fall
// outside the report's own range. The month is spelled because a numeric 03/10
// reads as October in Portugal and March in the US.
function reportIssueDate(issue, lang) {
  const raw = issue.started_at || issue.created_at;
  if (!raw) return '—';
  const d = new Date(raw);
  if (isNaN(d)) return '—';
  return d.toLocaleDateString(lang === 'pt' ? 'pt-PT' : 'en-GB', {day:'2-digit', month:'short'});
}

// dayNumber turns a YYYY-MM-DD string into whole days since the epoch.
//
// Both sides are built with Date.UTC so the two dates are compared as plain
// calendar days and neither can slip by one according to where the reader is --
// the same reason formatDateShort avoids new Date(iso) above. Returns null for
// anything unparseable rather than NaN, so a missing date leaves the countdown
// off the tile instead of rendering it as NaN days.
function dayNumber(iso) {
  const [y, m, d] = String(iso || '').slice(0, 10).split('-').map(Number);
  return y && m && d ? Math.floor(Date.UTC(y, m - 1, d) / 86400000) : null;
}

function MaintenanceQueue({ title, list, t, emptyText, canManage, handlers, testId }) {
  if (!list.length) return null;
  return (
    <div className={`maint-queue maint-queue-${testId}`} data-maint-section={testId}>
      <div className="maint-queue-head">
        <span className="maint-queue-dot" aria-hidden="true"/>
        <h2>{title}</h2>
        <span className="maint-queue-count">{list.length}</span>
      </div>
      <div className="maint-grid">
        {list.map((s, i) => (
          <MaintenanceDeviceCard key={s.id} s={s} t={t} index={i} canManage={canManage} {...handlers}/>
        ))}
      </div>
      {emptyText && <div className="empty-state">{Ic.box}<p>{emptyText}</p></div>}
    </div>
  );
}

function MaintenancePage({ token, currentUser, departments, setToast, onCountsUpdate, onRequestIssue }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const canManage = currentUser?.role === 'manager';
  const todayStr = localISODate(new Date());

  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState('');
  const [epoch, setEpoch] = useState(0);
  const [editing, setEditing] = useState(null);   // a schedule, or 'new'
  const [completing, setCompleting] = useState(null);
  const [historyFor, setHistoryFor] = useState(null);
  // Who each check can be assigned to. Fetched here rather than taken from the
  // app's user list, which is a page of its own -- a truncated roster would leave
  // a technician silently missing from the dropdown and their checks quietly
  // stuck on "all technicians".
  const [staff, setStaff] = useState([]);
  // Which kind of equipment the queue is showing: '' for all, or a type. Reset
  // when the page is left, like every other filter in the app.
  const [equipFilter, setEquipFilter] = useState('');
  // Which of the four groups the tiles are filtering down to, or null for all of
  // them. Separate from equipFilter because the two answer different questions --
  // what kind of equipment, and what state -- and both can be set at once.
  const [stateFilter, setStateFilter] = useState(null);

  const refresh = () => setEpoch(n => n + 1);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    apiFetch('/maintenance/schedules?recent=10', {token})
      .then(res => {
        if (cancelled) return;
        setData(res);
        setErr('');
        if (onCountsUpdate) onCountsUpdate(res.due_count || 0);
      })
      .catch(ex => { if (!cancelled) setErr(ex.message); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [token, epoch]);

  // Only a manager assigns anybody, so only a manager pays for the roster.
  useEffect(() => {
    if (!canManage) return;
    let cancelled = false;
    apiFetch('/users?limit=1000', {token})
      .then(res => { if (!cancelled) setStaff((res.users || []).filter(u => u.active)); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [token, canManage]);

  if (loading && !data) {
    return <div className="table-card"><div className="empty-state">{Ic.tool}<p>{t.analyticsLoading}</p></div></div>;
  }

  if (err) {
    /* The server's own message, but as an error rather than as an empty state.
       An .empty-state is the app's way of saying "there is genuinely nothing
       here", in grey, with nothing to click -- so a failed request used to print
       "network down" in exactly that shape and look like an answer. It also left
       onCountsUpdate uncalled, because due_count arrives on the success path, so
       the sidebar badge kept whatever it last knew instead of being told the
       count could not be read. refresh() puts the request back in front of the
       effect; nothing else has to change for it to actually re-run. */
    return (
      <div>
        <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,display:'flex',alignItems:'center',gap:10}}>
          <span style={{flex:1}}>{err}</span>
          <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
            onClick={refresh}>{t.tryAgain}</button>
        </div>
      </div>
    );
  }

  const all        = data?.schedules || [];

  // The equipment type filter is applied here rather than asked of the server.
  // This list is the one listing that is not paginated -- ListSchedules takes no
  // limit and the whole schedule arrives in one response -- so every device is
  // already in hand and there is nothing a server round trip would add.
  //
  // It also keeps the sidebar badge honest: due_count is computed over every
  // active device the caller may see, so narrowing the view cannot narrow the
  // count of work waiting. The badge answers "is anything due", the tiles answer
  // "what am I looking at", and only the second one follows the filter.
  const shown      = equipFilter ? all.filter(s => s.equipment_type === equipFilter) : all;
  const active     = shown.filter(s => s.active);

  // The four groups, counted over everything the equipment filter left. These are
  // also the tiles' numbers, which is why they are kept apart from what is on
  // screen: a tile that counted only the selected group would read 0 on the other
  // three, and a row of zeroes is a dead end -- you could not see what else there
  // was to switch to. So the tiles always report the real size of each group and
  // act as the navigation, and stateFilter only decides which groups are listed
  // underneath them.
  const groups = {
    overdue:   active.filter(s => s.due_state === 'overdue'),
    soon:      active.filter(s => s.due_state === 'due_soon'),
    scheduled: active.filter(s => s.due_state === 'scheduled'),
    retired:   shown.filter(s => !s.active),
  };
  const groupVisible = key => stateFilter === null || stateFilter === key;

  // Recent checks follow the filter too, so the table under the queue describes
  // the same equipment the queue does. The device is looked up in the unfiltered
  // list: a check whose device is currently filtered out still needs its name if
  // it is going to be shown, and a check on a hidden device must not resolve to a
  // bare dash.
  const shownIds   = new Set(shown.map(s => s.id));
  const recent     = (data?.recent || []).filter(c => shownIds.has(c.schedule_id));

  // Counts on the chips, so the filter says what is behind it before it is used.
  const typeChips = [
    { key: '',        label: t.maintenanceFilterAll, n: all.length },
    { key: 'printer', label: t.maintenancePrinters,  n: all.filter(s => s.equipment_type === 'printer').length },
    { key: 'phone',   label: t.maintenancePhones,    n: all.filter(s => s.equipment_type === 'phone').length },
  ];

  const handlers = {
    onComplete: setCompleting,
    onHistory:  setHistoryFor,
    onEdit:     setEditing,
  };

  // The check is filed the moment the dialog is saved, before the issue question
  // is even asked. Recording it second meant closing the issue form could lose a
  // fault the technician had already reported, and left the check dialog sitting
  // on top of the issue form. Filing first also means cancelling the issue form is
  // harmless: the fault is on record with its note, just not linked to a ticket.
  async function complete({schedule, outcome, note, doneOn, raiseIssue}) {
    setCompleting(null);

    let check;
    try {
      const res = await apiFetch(`/maintenance/schedules/${schedule.id}/checks`, {method:'POST', token,
        body:{outcome, note, done_on: doneOn || todayStr}});
      check = res.check;
    } catch(ex) {
      setToast('Error: ' + ex.message);
      return;
    }
    setToast(t.maintenanceCheckSaved);
    refresh();

    if (!raiseIssue) return;

    onRequestIssue({
      prefill: {
        dept: schedule.department_name,
        type:  maintenanceIssueType(schedule.equipment_type),
        // Named first so the issue says which machine it is about; the check it
        // links to says the rest.
        problem: `${schedule.device_name} — `,
      },
      onLinked: async issueId => {
        try {
          await apiFetch(`/maintenance/checks/${check.id}/link`, {method:'POST', token,
            body:{issue_id: issueId}});
          setToast(t.maintenanceIssueLogged);
          refresh();
        } catch(ex) { setToast('Error: ' + ex.message); }
      },
    });
  }

  return (
    <div>
      <div className="table-card" style={{marginBottom:18,padding:'14px 20px',display:'flex',alignItems:'center',gap:14,flexWrap:'wrap'}}>
        <p style={{fontSize:13,color:'var(--gray-400)',margin:0,flex:1,minWidth:260}}>{t.maintenanceSubtitle}</p>
        {/* Hidden until there is something to filter: with no equipment at all,
            one chip that can only be selected is a control that does nothing. */}
        {all.length > 0 && (
          <div className="filter-chips" role="group" aria-label={t.maintenanceEquipment}>
            {typeChips.map(c => (
              <button key={c.key || '__all__'} className={`chip ${equipFilter === c.key ? 'active' : ''}`}
                aria-pressed={equipFilter === c.key} onClick={()=>setEquipFilter(c.key)}>
                {c.label} <span style={{opacity:0.6}}>{c.n}</span>
              </button>
            ))}
          </div>
        )}
        {canManage && (
          <button className="btn btn-primary" style={{padding:'7px 14px',fontSize:12.5}}
            onClick={()=>setEditing('new')}>
            {Ic.plus} {t.maintenanceAddDevice}
          </button>
        )}
      </div>

      {/* Only once there is something to count. Four grey zeroes above "no
          equipment yet" is noise, and the empty state already says it.

          Each tile is a button that narrows the queue to its own group, and
          clicking the selected one puts everything back. Real buttons rather than
          divs with a role, so keyboard activation and focus come for free rather
          than being reimplemented. */}
      {all.length > 0 && (
        <div className="maint-tiles">
          {[
            { key: 'overdue',   label: t.maintenanceOverdue },
            { key: 'soon',      label: t.maintenanceDueSoon },
            { key: 'scheduled', label: t.maintenanceScheduled },
            { key: 'retired',   label: t.maintenanceRetired },
          ].map(x => {
            const n = groups[x.key].length;
            const on = stateFilter === x.key;
            return (
              <button key={x.key} type="button"
                className={`maint-tile ${x.key}${n ? ' live' : ''}${on ? ' is-filter' : ''}`}
                aria-pressed={on}
                onClick={()=>setStateFilter(on ? null : x.key)}>
                <span className="maint-tile-label"><span className="stat-dot" aria-hidden="true"/>{x.label}</span>
                <span className="maint-tile-value"><CountUp value={n}/></span>
              </button>
            );
          })}
        </div>
      )}

      {/* Three different emptinesses, and they need different words: nothing is on
          the schedule at all, nothing of the kind just filtered to, or the group
          picked on the tiles turned out to be empty. The middle one names the
          equipment type so it is obvious the filter emptied the page rather than
          the schedule being lost. */}
      {!all.length ? (
        <div className="empty-state">{Ic.tool}<p>{t.maintenanceNone}</p></div>
      ) : !shown.length ? (
        <div className="empty-state">{Ic.tool}
          <p>{equipFilter === 'phone' ? t.maintenanceNoPhones : t.maintenanceNoPrinters}</p>
        </div>
      ) : stateFilter !== null && groups[stateFilter].length === 0 ? (
        // A group with nothing in it still gets an answer, rather than the page
        // just going blank under a highlighted tile.
        <div className="empty-state">{Ic.tool}<p>{t.maintenanceNothingInGroup}</p></div>
      ) : (
        <>
          <MaintenanceQueue title={t.maintenanceOverdue}   list={groupVisible('overdue')   ? groups.overdue   : []} t={t} canManage={canManage} handlers={handlers} testId="overdue"/>
          <MaintenanceQueue title={t.maintenanceDueSoon}   list={groupVisible('soon')      ? groups.soon      : []} t={t} canManage={canManage} handlers={handlers} testId="soon"/>
          <MaintenanceQueue title={t.maintenanceScheduled} list={groupVisible('scheduled') ? groups.scheduled : []} t={t} canManage={canManage} handlers={handlers} testId="scheduled"/>
          <MaintenanceQueue title={t.maintenanceRetired}   list={groupVisible('retired')   ? groups.retired   : []} t={t} canManage={canManage} handlers={handlers} testId="retired"/>
        </>
      )}

      {/* Hidden when the filter matched nothing: "no checks recorded yet" would be
          a lie, there are plenty -- just none for this equipment. */}
      {shown.length > 0 && (
        <div style={{marginTop:26}}>
          <div style={{display:'flex',alignItems:'baseline',gap:8,marginBottom:10}}>
            <h2 style={{fontSize:15,margin:0}}>{t.maintenanceRecent}</h2>
          </div>
          {!recent.length ? (
            <div className="empty-state">{Ic.clock}<p>{t.maintenanceRecentEmpty}</p></div>
          ) : (
            <div className="table-card">
              <TableScroll label={t.maintenanceRecent}>
                <table>
                  <thead>
                    <tr>
                      <th>{t.date}</th><th>{t.maintenanceEquipment}</th><th>{t.maintenanceOutcome}</th>
                      <th>{t.maintenanceNote}</th><th>{t.loggedBy}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {recent.map(c => {
                      const device = all.find(s => s.id === c.schedule_id);
                      return (
                        <tr key={c.id}>
                          <td style={{whiteSpace:'nowrap',fontSize:12.5,color:'var(--gray-500)'}}>
                            {formatDateShort(c.done_on)}
                          </td>
                          <td>
                            {device ? (
                              <span style={{fontSize:12.5}}>
                                {device.device_name}
                                <span style={{color:'var(--gray-400)'}}> · {device.department_name}</span>
                              </span>
                            ) : '—'}
                          </td>
                          <td>
                            <span className={`badge ${c.outcome==='fault' ? 'badge-pending' : 'badge-ok'}`}>
                              {c.outcome==='fault' ? t.maintenanceFault : t.maintenanceAllFine}
                            </span>
                            {c.issue_label && (
                              <div style={{fontSize:11.5,color:'var(--gray-400)',marginTop:2}}>{c.issue_label}</div>
                            )}
                          </td>
                          <td style={{maxWidth:260,fontSize:12.5,color:'var(--gray-600)'}}>{c.note || '—'}</td>
                          <td style={{fontSize:12.5}}>{shortName(c.done_by_name)}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </TableScroll>
            </div>
          )}
        </div>
      )}

      {completing && (
        <MaintenanceCheckDialog schedule={completing} t={t} today={todayStr}
          onCancel={()=>setCompleting(null)} onSave={complete}/>
      )}
      {editing && (
        <MaintenanceScheduleDialog schedule={editing === 'new' ? null : editing}
            departments={departments} staff={staff} t={t} token={token} setToast={setToast}
          onDone={()=>{setEditing(null); refresh();}} onDeleted={()=>{setEditing(null); refresh();}}/>
      )}
      {historyFor && (
        <MaintenanceHistoryDialog schedule={historyFor} token={token} t={t} onClose={()=>setHistoryFor(null)}/>
      )}
    </div>
  );
}

// ── Record a check ──
function MaintenanceCheckDialog({ schedule, t, today, onCancel, onSave }) {
  const [outcome, setOutcome] = useState('ok');
  const [note, setNote] = useState('');
  const [doneOn, setDoneOn] = useState(today);
  const [raiseIssue, setRaiseIssue] = useState(true);
  const [saving, setSaving] = useState(false);

  // Escape is left live while saving: there is nothing to lose here, the note
  // and the outcome are re-entered from the card rather than recovered.
  const panelRef = useDialog({onClose});

  async function save() {
    if (saving) return;
    setSaving(true);
    try {
      await onSave({schedule, outcome, note, doneOn, raiseIssue: outcome === 'fault' && raiseIssue});
    } finally { setSaving(false); }
  }

  const typeLabel = schedule.equipment_type === 'phone' ? t.maintenancePhone : t.maintenancePrinter;

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && onCancel()}>
      <div className="modal" style={{maxWidth:460}} ref={panelRef} role="dialog" aria-modal="true"
        aria-label={t.maintenanceCompleteTitle}>
        <div className="modal-header">
          <div>
            <div style={{fontSize:15,fontWeight:600}}>{t.maintenanceCompleteTitle}</div>
            <div style={{fontSize:12.5,color:'var(--gray-400)',marginTop:2}}>
              {t.maintenanceCompleteFor.replace('{name}', `${schedule.device_name} · ${typeLabel} · ${schedule.department_name}`)}
            </div>
          </div>
          <button className="icon-btn" onClick={onCancel}>{Ic.x}</button>
        </div>
        <div className="modal-body" style={{display:'grid',gap:14}}>
          <div>
            {/* A caption for the two outcome buttons. */}
            <div className="form-group-h" id="maint-outcome-h" style={{fontSize:12.5}}>{t.maintenanceOutcome}</div>
            <div style={{display:'flex',gap:8,marginTop:6}} role="group" aria-labelledby="maint-outcome-h">
              <button className={`chip ${outcome==='ok' ? 'active' : ''}`} aria-pressed={outcome==='ok'}
                onClick={()=>setOutcome('ok')}>
                {Ic.check} {t.maintenanceOutcomeOk}
              </button>
              <button className={`chip ${outcome==='fault' ? 'active' : ''}`} aria-pressed={outcome==='fault'}
                onClick={()=>setOutcome('fault')}>
                {Ic.bell} {t.maintenanceOutcomeFault}
              </button>
            </div>
          </div>

          <div>
            <label htmlFor="maint-note" style={{fontSize:12.5,fontWeight:600,color:'var(--gray-600)'}}>{t.maintenanceNote}</label>
            <textarea id="maint-note" className="form-control" style={{marginTop:6,minHeight:70}} maxLength={500}
              value={note} onChange={e=>setNote(e.target.value)}
              placeholder={outcome === 'fault' ? t.maintenanceNoteFaultHint : t.maintenanceNoteOkHint}/>
          </div>

          {outcome === 'fault' && (
            <label style={{display:'flex',gap:9,alignItems:'flex-start',fontSize:13,cursor:'pointer'}}>
              <input id="maint-raise-issue" type="checkbox" checked={raiseIssue} onChange={e=>setRaiseIssue(e.target.checked)}
                style={{marginTop:2}}/>
              <span>
                <strong style={{fontWeight:500}}>{t.maintenanceAlsoLogIssue}</strong>
                <div style={{fontSize:12,color:'var(--gray-400)',marginTop:2}}>{t.maintenanceAlsoLogIssueHint}</div>
              </span>
            </label>
          )}

          <div>
            <label htmlFor="maint-done-on" style={{fontSize:12.5,fontWeight:600,color:'var(--gray-600)'}}>{t.maintenanceDoneOn}</label>
            <input id="maint-done-on" type="date" className="form-control" style={{marginTop:6,width:'auto'}} max={today}
              value={doneOn} onChange={e=>setDoneOn(e.target.value)}/>
            <div style={{fontSize:12,color:'var(--gray-400)',marginTop:3}}>{t.maintenanceDoneOnHint}</div>
          </div>
        </div>
        <div className="modal-footer">
          <button className="btn btn-ghost" onClick={onCancel} disabled={saving}>{t.cancel}</button>
          <button className="btn btn-primary" onClick={save} disabled={saving}>
            {saving ? '…' : t.maintenanceMarkDone}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Add or edit a device ──
function MaintenanceScheduleDialog({ schedule, departments, staff, t, token, setToast, onDone, onDeleted }) {
  const isNew = !schedule;
  const [name, setName] = useState(schedule?.device_name || '');
  const [type, setType] = useState(schedule?.equipment_type || 'printer');
  const [deptId, setDeptId] = useState(schedule?.department_id || (departments[0]?.id ?? ''));
  const [interval, setInterval] = useState(schedule?.interval_days ?? 30);
  const [custom, setCustom] = useState(
    schedule ? !MAINTENANCE_PRESETS.some(p => p.days === schedule.interval_days) : false);
  const [days, setDays] = useState(schedule?.interval_days ?? 30);
  // Retirement lives here rather than on the card: taking a device out of the
  // queue while keeping its history is a deliberate choice about one device, so
  // it belongs beside the other settings for that device. It has to be settable
  // both ways -- a device retired by mistake has to be recoverable.
  const [active, setActive] = useState(schedule ? schedule.active !== false : true);
  // ' means every technician. A string rather than a number so the empty case is
  // distinct from id 0, and so it maps straight onto the option's value.
  const [assignee, setAssignee] = useState(schedule && schedule.assignee_id ? String(schedule.assignee_id) : '');
  const [err, setErr] = useState('');
  const [saving, setSaving] = useState(false);

  const panelRef = useDialog({onClose: saving ? null : onDone});

  async function save() {
    if (saving) return;
    setSaving(true);
    setErr('');
    const body = {
      device_name: name,
      equipment_type: type,
      department_id: Number(deptId),
      interval_days: Number(custom ? days : interval),
      // Only meaningful on an edit: a new device is always active, and sending
      // it as retired would create something nobody can find.
      ...(isNew ? {} : {active}),
      assignee_id: assignee ? Number(assignee) : null,
    };
    try {
      if (isNew) {
        await apiFetch('/maintenance/schedules', {method:'POST', token, body});
      } else {
        await apiFetch(`/maintenance/schedules/${schedule.id}`, {method:'PATCH', token, body});
      }
      setToast(t.maintenanceScheduleSaved);
      onDone();
    } catch(ex) {
      // A validation failure comes back keyed by field; show the first message
      // rather than a bare "request failed", which tells the reader nothing.
      setErr(ex.message || 'Error');
    } finally { setSaving(false); }
  }

  // Printers and phones do not come round at the same rate, so picking the class
  // moves the cadence with it -- but only when the cadence is still one of the
  // presets. Overriding an interval the manager deliberately chose would be rude,
  // and a custom one cannot be guessed at in the other direction either.
  function pickType(next) {
    setType(next);
    if (custom) return;
    if (next === 'printer' && interval !== 30 && MAINTENANCE_PRESETS.some(p => p.days === interval)) {
      setInterval(30);
    }
    if (next === 'phone' && interval !== 90 && MAINTENANCE_PRESETS.some(p => p.days === interval)) {
      setInterval(90);
    }
  }

  async function remove() {
    // The count comes off the schedule row, so the prompt can say what deleting it
    // costs rather than quoting a number nobody kept.
    const count = schedule && schedule.check_count != null ? schedule.check_count : 0;
    if (!confirm(t.maintenanceDeleteConfirm.replace('{name}', schedule.device_name).replace('{count}', count))) return;
    setSaving(true);
    try {
      await apiFetch(`/maintenance/schedules/${schedule.id}`, {method:'DELETE', token});
      setToast(t.maintenanceScheduleDeleted);
      onDeleted();
    } catch(ex) { setErr(ex.message); }
    finally { setSaving(false); }
  }

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && onDone()}>
      <div className="modal" style={{maxWidth:460}} ref={panelRef} role="dialog" aria-modal="true"
        aria-label={isNew ? t.maintenanceAddDevice : t.maintenanceEditDevice}>
        <div className="modal-header">
          <div style={{fontSize:15,fontWeight:600}}>
            {isNew ? t.maintenanceAddDevice : t.maintenanceEditDevice}
          </div>
          <button className="icon-btn" onClick={onDone}>{Ic.x}</button>
        </div>
        <div className="modal-body" style={{gap:16}}>
          <div className="form-group">
            <label htmlFor="maint-name">{t.maintenanceDeviceName}</label>
            <input id="maint-name" className="form-control" maxLength={80} autoFocus
              value={name} onChange={e=>setName(e.target.value)} placeholder={t.maintenanceDeviceNameHint}/>
            <div className="maint-hint">{t.maintenanceDeviceNameHint}</div>
          </div>

          <div className="form-group">
            <div className="form-group-h" id="maint-equipment-h">{t.maintenanceEquipment}</div>
            <div className="maint-choices" role="group" aria-labelledby="maint-equipment-h">
              <button type="button" className={`maint-choice ${type==='printer' ? 'on' : ''}`}
                aria-pressed={type==='printer'}
                onClick={()=>pickType('printer')}>
                {Ic.printer} {t.maintenancePrinter}
              </button>
              <button type="button" className={`maint-choice ${type==='phone' ? 'on' : ''}`}
                aria-pressed={type==='phone'}
                onClick={()=>pickType('phone')}>
                {Ic.phone} {t.maintenancePhone}
              </button>
            </div>
          </div>

          <div className="form-group">
            <label htmlFor="maint-dept">{t.maintenanceDepartment}</label>
            <select id="maint-dept" className="form-control" value={deptId}
              onChange={e=>setDeptId(e.target.value)}>
              {(departments || []).map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
            </select>
          </div>

          {/* A select, not chips. Five presets plus a custom escape will not fill a
              row evenly however they wrap, and this sits directly under another
              select -- so the same control twice reads as deliberate rather than
              as a fallback for the one field that happened to run out of room. */}
          <div className="form-group">
            <label htmlFor="maint-interval">{t.maintenanceInterval}</label>
            <select id="maint-interval" className="form-control" value={custom ? 'custom' : String(interval)}
              onChange={e=>{
                if (e.target.value === 'custom') { setCustom(true); setDays(interval); }
                else { setCustom(false); setInterval(Number(e.target.value)); }
              }}>
              {MAINTENANCE_PRESETS.map(p => (
                <option key={p.key} value={p.days}>
                  {t['maintenanceInterval' + p.key.charAt(0).toUpperCase() + p.key.slice(1)]}
                </option>
              ))}
              <option value="custom">{t.maintenanceIntervalCustom}</option>
            </select>
            {custom && (
              <>
                {/* Its own name, not the interval label above: that label belongs to the
                    select, and one label cannot cover two controls. Same wording
                    as the option that revealed this field. */}
                <input id="maint-interval-days" type="number" className="form-control" min={7} max={730}
                  aria-label={t.maintenanceIntervalCustom}
                  value={days} onChange={e=>setDays(e.target.value)}/>
                <div className="maint-hint">
                  {t.maintenanceIntervalDays.replace('{days}', days || '—')}
                </div>
              </>
            )}
          </div>

          <div className="form-group">
            <label htmlFor="maint-assignee">{t.maintenanceAssignTo}</label>
            <select id="maint-assignee" className="form-control" value={assignee} onChange={e=>setAssignee(e.target.value)}>
              <option value="">{t.maintenanceAllTechnicians}</option>
              {(staff || []).map(u => (
                <option key={u.id} value={u.id}>{shortName(u.name)}</option>
              ))}
            </select>
            <div className="maint-hint">{t.maintenanceAssignHint}</div>
          </div>

          {!isNew && (
            <label style={{display:'flex',gap:9,alignItems:'flex-start',fontSize:13,cursor:'pointer',
              borderTop:'1px solid var(--gray-200)',paddingTop:16}}>
              <input id="maint-active" type="checkbox" checked={active} onChange={e=>setActive(e.target.checked)}
                style={{marginTop:2}}/>
              <span>
                <strong style={{fontWeight:500}}>{t.maintenanceInQueue}</strong>
                <div className="maint-hint" style={{marginTop:2}}>{t.maintenanceRetireHint}</div>
              </span>
            </label>
          )}

          {err && <div style={{fontSize:12.5,color:'var(--red)'}}>{err}</div>}
        </div>
        <div className="modal-footer">
          {!isNew && (
            <button className="btn btn-ghost" style={{marginRight:'auto',color:'var(--red)'}}
              onClick={remove} disabled={saving}>{t.maintenanceDelete}</button>
          )}
          <button className="btn btn-ghost" onClick={onDone} disabled={saving}>{t.cancel}</button>
          <button className="btn btn-primary" onClick={save}
            disabled={saving || !name.trim() || !deptId}>
            {/* Named for what it does rather than always "Save changes", which on an
                empty form reads as saving something. */}
            {saving ? '…' : isNew ? t.maintenanceAddDevice : t.saveChanges}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── One device's history ──
function MaintenanceHistoryDialog({ schedule, token, t, onClose }) {
  const [checks, setChecks] = useState(null);
  const [err, setErr] = useState('');

  useEffect(() => {
    let cancelled = false;
    apiFetch(`/maintenance/schedules/${schedule.id}/checks`, {token})
      .then(res => { if (!cancelled) setChecks(res.checks || []); })
      .catch(ex => { if (!cancelled) setErr(ex.message); });
    return () => { cancelled = true; };
  }, [schedule.id, token]);

  const typeLabel = schedule.equipment_type === 'phone' ? t.maintenancePhone : t.maintenancePrinter;

  const panelRef = useDialog({onClose});

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && onClose()}>
      <div className="modal" style={{maxWidth:520}} ref={panelRef} role="dialog" aria-modal="true"
        aria-label={t.maintenanceHistory}>
        <div className="modal-header">
          <div>
            <div style={{fontSize:15,fontWeight:600}}>{schedule.device_name}</div>
            <div style={{fontSize:12.5,color:'var(--gray-400)',marginTop:2}}>
              {typeLabel} · {schedule.department_name} · {t.maintenanceIntervalDays.replace('{days}', schedule.interval_days)}
            </div>
          </div>
          <button className="icon-btn" onClick={onClose}>{Ic.x}</button>
        </div>
        <div className="modal-body">
          {err ? <div style={{fontSize:12.5,color:'var(--red)'}}>{err}</div>
            : !checks ? <p style={{color:'var(--gray-400)',fontSize:13}}>{t.analyticsLoading}</p>
            : !checks.length ? <p style={{color:'var(--gray-400)',fontSize:13}}>{t.maintenanceNoHistory}</p>
            : (
              <div style={{display:'grid',gap:9}}>
                {checks.map(c => (
                  <div key={c.id} style={{display:'flex',gap:11,alignItems:'flex-start',paddingBottom:9,borderBottom:'1px solid var(--gray-200)'}}>
                    <span className={`badge ${c.outcome==='fault' ? 'badge-pending' : 'badge-ok'}`}
                      style={{flexShrink:0,minWidth:62,justifyContent:'center'}}>
                      {c.outcome==='fault' ? t.maintenanceFault : t.maintenanceAllFine}
                    </span>
                    <div style={{flex:1,minWidth:0}}>
                      <div style={{fontSize:12.5,color:'var(--gray-500)'}}>
                        {formatDateShort(c.done_on)} · {t.maintenanceBy.replace('{name}', shortName(c.done_by_name))}
                      </div>
                      {c.note && <div style={{fontSize:13,marginTop:2}}>{c.note}</div>}
                      {c.issue_label && (
                        <div style={{fontSize:11.5,color:'var(--blue)',marginTop:2}}>↳ {c.issue_label}</div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
        </div>
        <div className="modal-footer">
          <button className="btn btn-ghost" onClick={onClose}>{t.cancel}</button>
        </div>
      </div>
    </div>
  );
}


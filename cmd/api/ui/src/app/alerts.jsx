/* ════════════════ ALERTS PAGE ════════════════ */
function AlertsPage({ currentUser, token, onCountsUpdate, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const isManager = currentUser.role === 'manager';
  const [filter, setFilter] = useState('pending');
  const [solvingId, setSolvingId] = useState(null);
  const [solvingTvId, setSolvingTvId] = useState(null);
  const [solutionText, setSolutionText] = useState('');
  const [saving, setSaving] = useState(false);

  /* This page fetches its own alerts. It used to receive them from App, which
     also needed them only for the sidebar badge -- so App held every alert card
     and its nested list of occurrences in memory to draw one number.

     The status filter goes to the server, so switching between Pending and Solved
     asks for that set rather than narrowing a set it already has. The endpoint
     answers with both kinds in one request and its own pending counts, which is
     what the chips are drawn from -- those count every outstanding alert, not
     just the loaded page, so the badge never empties itself when you look at the
     solved ones. */
  // Re-read after a solve or a delete so the card that was just dealt with leaves
  // the list it was on. Part of the filter key, which also drops the limit back
  // to one page: the card that was solved is no longer in this set at all, so
  // there is nothing to be gained by keeping the pages that were already loaded.
  const [epoch, setEpoch] = useState(0);
  const refresh = () => setEpoch(n => n + 1);

  const paged = usePagedList(limit => `/alerts?limit=${limit}&status=${filter}`, token,
    {rowsKey:'alerts', filterKey: `${filter}|${epoch}`});

  // `rows` is the alert array itself; the counts and the second list live alongside
  // it in the response, so they are read from there rather than off the array.
  //
  // These two are declared before the effect below, not after it, and that ordering
  // is load-bearing. A dependency array is evaluated while rendering -- before the
  // effect's body ever runs -- so naming `data` in it while `data` is still a const
  // awaiting its declaration is a ReferenceError, not a stale read. It read as
  // harmless for as long as Babel was compiling this file, because Babel's preset-env
  // quietly rewrote every const to var and the uninitialised read came back as
  // undefined instead of throwing: no crash, and a sidebar badge that never updated
  // after an alert was dealt with here. esbuild keeps the const, so the same line
  // took the whole page down.
  const data = paged.res;
  const list = paged.rows;

  // The sidebar badge and the Analytics sentence are drawn from a count the app
  // holds, and this page is now the only thing that knows when an alert has been
  // dealt with. Without handing the fresh figure back, signing off the last
  // recurring alert here would leave a "1" in the sidebar until the next reload.
  useEffect(() => {
    if (data && onCountsUpdate) onCountsUpdate(data.pending_count || 0);
  }, [data, onCountsUpdate]);

  const tvList = data ? (data.tv_swap_alerts || []) : [];
  // The server has already filtered both lists by status, so this is what is
  // on screen -- no narrowing needed here.
  const tvFiltered = tvList;
  // The chips count every alert of each kind, across all pages rather than the
  // loaded one, and across both kinds of alert.
  const pendingCount = data ? (data.pending_count || 0) + (data.tv_swap_pending_count || 0) : 0;
  const solvedCount  = data ? (data.solved_count  || 0) + (data.tv_swap_solved_count  || 0) : 0;
  // Section headings: how many this filter holds, which is `total` because the
  // request was already filtered.
  const tvShownTotal  = data ? (data.tv_swap_total || 0) : 0;
  const shownTotal    = data ? (data.total || 0) : 0;

  async function submitSolve(id) {
    const trimmed = solutionText.trim();
    if (!trimmed) return;
    setSaving(true);
    try {
      await apiFetch(`/alerts/${id}/solve`, {method:'PATCH', body:{solution:trimmed}, token});
      setSolvingId(null);
      setSolutionText('');
      setToast(t.alertSolved);
      refresh();
    } catch(ex) { setToast('Error: ' + ex.message); }
    finally { setSaving(false); }
  }

  async function removeAlert(id) {
    try {
      await apiFetch(`/alerts/${id}`, {method:'DELETE', token});
      setToast(t.alertDeleted);
      refresh();
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function submitTvSolve(id) {
    const trimmed = solutionText.trim();
    if (!trimmed) return;
    setSaving(true);
    try {
      await apiFetch(`/tv-swap-alerts/${id}/solve`, {method:'PATCH', body:{solution:trimmed}, token});
      setSolvingTvId(null);
      setSolutionText('');
      setToast(t.tvSwapAlertSolved);
      refresh();
    } catch(ex) { setToast('Error: ' + ex.message); }
    finally { setSaving(false); }
  }

  /* A dead /alerts is not an empty list of alerts. Everything below is drawn from
     `data`, and with no data those counts are zeros -- so a failed request left the
     chips reading "Pending (0) | Solved (0)", contradicting the sidebar badge that
     still held the real figure, and both sections showing "Loading analytics…"
     forever. There is nothing honest to draw without the answer, so the page says
     what happened once and offers to ask again.

     An early return rather than a branch inside the markup: the two sections below
     are long, and a ternary wrapped around them would either reindent all of it or
     leave the section headings -- which are outside the `!data` branches -- still
     announcing "0 of". */
  if (paged.error) {
    return (
      <div>
        <div className="table-card" style={{marginBottom:16,padding:'14px 20px'}}>
          <p style={{fontSize:13,color:'var(--gray-400)',margin:0}}>{t.alertsSubtitle}</p>
        </div>
        <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,marginBottom:16,display:'flex',alignItems:'center',gap:10}}>
          <span style={{flex:1}}>{paged.error}</span>
          <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
            onClick={refresh}>{t.tryAgain}</button>
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="table-card" style={{marginBottom:16,padding:'14px 20px'}}>
        <p style={{fontSize:13,color:'var(--gray-400)',margin:0}}>{t.alertsSubtitle}</p>
      </div>

      <div style={{display:'flex',gap:8,marginBottom:16}}>
        <button className={`chip ${filter==='pending'?'active':''}`} aria-pressed={filter==='pending'} onClick={()=>setFilter('pending')}>
          {t.alertsPending} ({pendingCount})
        </button>
        <button className={`chip ${filter==='solved'?'active':''}`} aria-pressed={filter==='solved'} onClick={()=>setFilter('solved')}>
          {t.alertsSolved} ({solvedCount})
        </button>
      </div>

      {/* ── Equipment swapped out ──
          Its own section above the recurring ones, because a swap is not the same
          kind of warning: it says a set was carried in to cover for a fault, so
          the room's own equipment is still broken. Nothing about it is recurring.
          The empty case is spelled out rather than left blank, so an absent
          section does not read as a section that failed to load. */}
      <div style={{display:'flex',alignItems:'baseline',gap:8,marginBottom:12}}>
        <h2 style={{fontSize:15,margin:0}}>{t.tvSwapAlertsTitle}</h2>
        <span style={{fontSize:12.5,color:'var(--gray-400)'}}>
          {tvShownTotal} {t.tvSwapAlertsOf}
        </span>
      </div>

      {!data ? (
        <div className="empty-state">{Ic.tv}<p>{t.analyticsLoading}</p></div>
      ) : tvFiltered.length === 0 ? (
        <div className="empty-state">{Ic.tv}
          <p>{filter==='pending' ? t.tvSwapAlertsEmpty : t.tvSwapAlertsSolvedEmpty}</p>
        </div>
      ) : tvFiltered.map(alert => (
        <div className="table-card" key={'tv'+alert.id} style={{marginBottom:16}}>
          <div className="table-header">
            <div style={{display:'flex',alignItems:'center',gap:10,minWidth:0}}>
              <span style={{color: alert.status==='solved' ? 'var(--green)' : 'var(--amber)'}}>{Ic.tv}</span>
              {/* The room named is the swap's source, not the issue's location.
                  The issue was logged where the fault was found, which is the
                  room that borrowed a working set; the room now left holding a
                  dud is the one that lent its own away, and that is the room
                  somebody has to walk to. Falls back to the issue's location so a
                  swap recorded without a source shows something rather than a
                  blank heading. */}
              <span className="table-title">{alert.from_room || alert.location}</span>
              <TypeBadge type={alert.type}/>
            </div>
            <div style={{display:'flex',alignItems:'center',gap:10}}>
              {isManager && alert.status==='pending' && solvingTvId!==alert.id && (
                <button className="btn btn-ghost" style={{padding:'4px 10px',fontSize:12}}
                  onClick={()=>{setSolvingTvId(alert.id); setSolutionText('');}}>
                  {t.markSolved}
                </button>
              )}
            </div>
          </div>

          <div style={{padding:'13px 20px',borderBottom: alert.status==='solved' || solvingTvId===alert.id ? 'none' : '1px solid var(--gray-200)'}}>
            {/* One {type} in the sentence, so a single replace covers it. The type is
                named rather than hard-coded as "TV", so this stays right if
                another type ever records swaps. */}
            <p style={{margin:'0 0 9px',fontSize:13.5}}>{t.tvSwapAlertBody.replace('{type}', alert.type)}</p>
            {alert.problem && (
              <p style={{margin:'0 0 9px',fontSize:13,color:'var(--gray-600)'}}>{alert.problem}</p>
            )}
            <div style={{display:'flex',flexWrap:'wrap',gap:'6px 18px',fontSize:12,color:'var(--gray-400)'}}>
              <span>{t.tvSwapAlertFrom} <strong style={{color:'var(--gray-900)'}}>{alert.from_room}</strong></span>
              <span>{t.tvSwapAlertTo} <strong style={{color:'var(--gray-900)'}}>{alert.to_room}</strong></span>
              <span>{t.date} {new Date(alert.swap_created_at).toLocaleString()}</span>
              <span>{t.loggedBy} {shortName(alert.logged_by_name)}</span>
            </div>
            {alert.notes && (
              <p style={{margin:'9px 0 0',fontSize:12.5,color:'var(--gray-600)',fontStyle:'italic'}}>{alert.notes}</p>
            )}
          </div>

          {alert.status === 'solved' && (
            <div style={{padding:'12px 20px',background:'var(--green-light)',borderTop:'1px solid var(--gray-200)'}}>
              <div style={{fontSize:11,fontWeight:600,color:'var(--green)',textTransform:'uppercase',letterSpacing:'0.07em',marginBottom:4}}>{t.finalSolution}</div>
              <div style={{fontSize:13,color:'var(--gray-900)'}}>{alert.solution}</div>
              <div style={{fontSize:11.5,color:'var(--gray-400)',marginTop:6}}>
                {t.solvedBy} <strong>{shortName(alert.solved_by_name)}</strong>
                {alert.solved_at && <> · {new Date(alert.solved_at).toLocaleString()}</>}
              </div>
            </div>
          )}

          {solvingTvId === alert.id && (
            <div style={{padding:'14px 20px',background:'var(--gray-50)',borderTop:'1px solid var(--gray-200)'}}>
              <label htmlFor="alert-tv-solution" style={{fontSize:11,fontWeight:600,color:'var(--gray-600)',textTransform:'uppercase',letterSpacing:'0.07em'}}>{t.tvSwapSolvedWhat}</label>
              <textarea id="alert-tv-solution" className="form-control" rows={2} style={{marginTop:6,marginBottom:10,resize:'vertical'}}
                value={solutionText} onChange={e=>setSolutionText(e.target.value)}
                placeholder={t.tvSwapSolvedPlaceholder}/>
              <div style={{display:'flex',gap:8}}>
                <button className="btn btn-ghost" style={{padding:'6px 14px',fontSize:12.5}}
                  onClick={()=>{setSolvingTvId(null); setSolutionText('');}}>{t.cancel}</button>
                <button className="btn btn-primary" style={{padding:'6px 14px',fontSize:12.5}}
                  disabled={saving || !solutionText.trim()} onClick={()=>submitTvSolve(alert.id)}>
                  {saving ? '…' : t.confirmSolved}
                </button>
              </div>
            </div>
          )}
        </div>
      ))}

      {/* ── Recurring issues ── */}
      <div style={{display:'flex',alignItems:'baseline',gap:8,margin:'24px 0 12px'}}>
        <h2 style={{fontSize:15,margin:0}}>{t.alertsRecurringTitle}</h2>
        <span style={{fontSize:12.5,color:'var(--gray-400)'}}>
          {shownTotal} {t.tvSwapAlertsOf}
        </span>
      </div>

      {!data ? (
        <div className="empty-state">{Ic.bell}<p>{t.analyticsLoading}</p></div>
      ) : list.length === 0 ? (
        <div className="empty-state">{Ic.bell}<p>{filter==='pending' ? t.alertsEmpty : t.alertsSolvedEmpty}</p></div>
      ) : list.map(alert => {
        // The API always sends an array now, but a null here would take the
        // whole page down, and this is the one field the Alerts page cannot
        // do without.
        const issues = alert.issues || [];
        const isStale = issues.length === 0;
        return (
        <div className="table-card" key={alert.id} style={{marginBottom:16}}>
          <div className="table-header">
            <div style={{display:'flex',alignItems:'center',gap:10}}>
              <span style={{color: alert.status==='solved' ? 'var(--green)' : 'var(--amber)'}}>{Ic.bell}</span>
              <span className="table-title">{alert.location}</span>
              <TypeBadge type={alert.type}/>
            </div>
            <div style={{display:'flex',alignItems:'center',gap:10}}>
              <span style={{fontSize:12,color:'var(--gray-400)'}}>{issues.length} {t.alertsOccurrences}</span>
              {isStale && (
                <span className="badge" style={{background:'var(--gray-200)',color:'var(--gray-600)',fontSize:10,padding:'1px 7px'}}>
                  {t.alertStale}
                </span>
              )}
              {isManager && alert.status==='pending' && solvingId!==alert.id && (
                <button className="btn btn-ghost" style={{padding:'4px 10px',fontSize:12}}
                  onClick={()=>{setSolvingId(alert.id); setSolutionText('');}}>
                  {t.markSolved}
                </button>
              )}
              {isManager && (
                <button className="icon-btn" title={t.deleteAlert} onClick={()=>removeAlert(alert.id)}>{Ic.trash}</button>
              )}
            </div>
          </div>

          {alert.status === 'solved' && (
            <div style={{padding:'12px 20px',background:'var(--green-light)',borderBottom:'1px solid var(--gray-200)'}}>
              <div style={{fontSize:11,fontWeight:600,color:'var(--green)',textTransform:'uppercase',letterSpacing:'0.07em',marginBottom:4}}>{t.finalSolution}</div>
              <div style={{fontSize:13,color:'var(--gray-900)'}}>{alert.solution}</div>
              <div style={{fontSize:11.5,color:'var(--gray-400)',marginTop:6}}>
                {t.solvedBy} <strong>{shortName(alert.solved_by_name)}</strong> · {new Date(alert.solved_at).toLocaleString()}
              </div>
            </div>
          )}

          {solvingId === alert.id && (
            <div style={{padding:'14px 20px',background:'var(--gray-50)',borderBottom:'1px solid var(--gray-200)'}}>
              <label htmlFor="alert-solution" style={{fontSize:11,fontWeight:600,color:'var(--gray-600)',textTransform:'uppercase',letterSpacing:'0.07em'}}>{t.finalSolution}</label>
              <textarea id="alert-solution" className="form-control" rows={2} style={{marginTop:6,marginBottom:10,resize:'vertical'}}
                value={solutionText} onChange={e=>setSolutionText(e.target.value)}
                placeholder={t.finalSolutionPlaceholder}/>
              <div style={{display:'flex',gap:8}}>
                <button className="btn btn-ghost" style={{padding:'6px 14px',fontSize:12.5}} onClick={()=>setSolvingId(null)}>{t.cancel}</button>
                <button className="btn btn-primary" style={{padding:'6px 14px',fontSize:12.5}} disabled={saving || !solutionText.trim()} onClick={()=>submitSolve(alert.id)}>
                  {saving ? '…' : t.confirmSolved}
                </button>
              </div>
            </div>
          )}

          <table style={{tableLayout:'fixed'}}>
            <colgroup>
              <col style={{width:'14%'}}/>
              <col style={{width:'20%'}}/>
              <col style={{width:'34%'}}/>
              <col style={{width:'12%'}}/>
              <col style={{width:'20%'}}/>
            </colgroup>
            <thead>
              <tr>
                <th>{t.date}</th>
                <th>{t.problem}</th>
                <th>{t.resolution}</th>
                <th>{t.status}</th>
                <th>{t.loggedBy}</th>
              </tr>
            </thead>
            <tbody>
              {issues.map(issue => (
                <tr key={issue.id}>
                  <td style={{fontSize:12,color:'var(--gray-400)',whiteSpace:'nowrap'}}>
                    {new Date(issue.created_at).toLocaleString()}
                  </td>
                  <td>{issue.problem || '—'}
                    <TVSwapStrip swaps={issue.tv_swaps} t={t}/></td>
                  <td>{issue.resolution || '—'}</td>
                  <td><span className={`badge ${issue.status==='Ok'?'badge-ok':'badge-pending'}`}>{issue.status}</span></td>
                  <td>{shortName(issue.logged_by_name)}</td>
                </tr>
              ))}
            </tbody>
          </table>

          {/* Every issue this alert was raised for has since been deleted, so
              it is a leftover rather than a live problem. An empty table with
              headers and no rows reads as a bug, so say what it actually is and
              point at the one action that clears it. */}
          {isStale && (
            <div className="alert-stale">
              <p className="alert-stale-title">{t.alertStaleTitle}</p>
              <p className="alert-stale-body">{t.alertStaleBody}</p>
              {isManager && (
                <button className="btn btn-ghost" style={{fontSize:12.5,padding:'6px 14px'}}
                  onClick={()=>removeAlert(alert.id)}>
                  {t.alertStaleDismiss}
                </button>
              )}
            </div>
          )}
        </div>
      );})}

      {/* One footer for the page rather than one per section: both sections come
          from a single request and page together, so a reader who has loaded six
          pages has six pages of each. */}
      <LoadMore list={paged} t={t}/>
    </div>
  );
}

/* ── Third-party support: how long the company took ── */
/* Renders a number of minutes the way the support page shows it: days first
   once a handover runs past a day, then hours and minutes. Returns null for a
   missing duration, so callers can show a dash rather than a misleading "0m". */

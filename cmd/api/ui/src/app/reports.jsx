/* ════════════════ REPORTS PAGE ════════════════ */
function ReportsPage({ token, slaMinutes, issueTypes, theme }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  // Three modes, one page. Daily is the original; monthly and range aggregate
  // over a longer period. Each mode keeps its OWN date state, so switching back
  // and forth does not wipe a selection the user was building up -- a manager
  // flipping from "October" to "this week" and back expects October to still be
  // there.
  const [mode, setMode] = useState('daily');
  const [date, setDate]   = useState(localISODate(new Date()));
  const [month, setMonth] = useState(localISODate(new Date()).slice(0, 7));
  const todayISO = localISODate(new Date());
  const [from, setFrom]   = useState(() => {
    const d = new Date(); d.setDate(d.getDate() - 6);
    return localISODate(d);
  });
  const [to, setTo]       = useState(todayISO);
  // Issue-type filter. Empty string means "all types", which is the default and
  // the only value that makes the by-type chart meaningful; a single type turns
  // that chart into one bar, which is honest rather than useful, but the rest of
  // the report (summary, by technician) is still worth having for one type.
  const [typeFilter, setTypeFilter] = useState('');
  // Issue-status filter. "pending" is the open set: an issue stays Pending
  // until it is solved, so the filter is labelled Open / pending rather than
  // making the reader guess which of the two words matches the stored status.
  const [statusFilter, setStatusFilter] = useState('all');
  // The native <select> dropdown is drawn by the OS and ignores color-scheme on
  // Windows, so it opens as a white list with light text on it in dark mode.
  // A custom dropdown is the only way to control its palette, hence this.
  const [typeOpen, setTypeOpen] = useState(false);
  const typeRef = useRef(null);
  useEffect(() => {
    if (!typeOpen) return;
    function onDoc(e) {
      if (typeRef.current && !typeRef.current.contains(e.target)) setTypeOpen(false);
    }
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [typeOpen]);
  const [error,   setError]   = useState('');
  const [downloading, setDownloading] = useState(false);
  // Re-asks for the same selection. setDate(d => d) cannot do this: it sets the
  // state to the identical value, React bails out of the re-render, filterKey
  // never changes and the fetch effect never runs -- which left this button, and
  // the retry below it, doing nothing at all. A counter in the filter key is how
  // AlertsPage refreshes, and the point of it is that the path comes back
  // byte-identical, so the key is the only thing that can carry the request.
  const [epoch,   setEpoch]   = useState(0);

  // The period the current mode is actually reporting on, as a single string the
  // rest of the page can switch on without re-deriving it. Kept in sync with the
  // inputs above rather than computed, because the inputs are the source of truth
  // and the derived form would have to be rebuilt on every render.
  const filterKey = mode === 'daily'   ? `daily|${date}|${typeFilter}|${statusFilter}|${epoch}`
                  : mode === 'monthly' ? `monthly|${month}|${typeFilter}|${statusFilter}|${epoch}`
                  :                      `range|${from}|${to}|${typeFilter}|${statusFilter}|${epoch}`;

  function buildPath(limit) {
    const typeQ = typeFilter ? `&type=${encodeURIComponent(typeFilter)}` : '';
    const statusQ = `&status=${encodeURIComponent(statusFilter)}`;
    if (mode === 'daily')   return `/reports/daily?date=${date}&limit=${limit}${typeQ}${statusQ}`;
    if (mode === 'monthly') return `/reports/monthly?month=${month}&limit=${limit}${typeQ}${statusQ}`;
    return `/reports/range?from=${from}&to=${to}&limit=${limit}${typeQ}${statusQ}`;
  }

  /* The two issue tables page; everything else on this page does not.
     The summary tiles, the by-type and by-technician charts are aggregates over
     the whole day and the server computes them without regard to the limit, so
     the figures stay true while a busy day's rows are still being loaded. */
  const paged = usePagedList(buildPath, token, {
    // The report is one object rather than an array of rows, so `rows` holds that
    // object and the two tables inside it are what page. An object is not an
    // array, so it arrives as-is rather than being coerced.
    rowsKey: 'report',
    filterKey,
    onError: ex => setError(ex.message),
  });
  const report = paged.res && paged.res.report;
  const loading = paged.loading && !report;
  // Clears the banner as well as re-asking: the point of pressing it after a
  // failure is to find out whether the failure is over, so a request that
  // succeeds must be allowed to take the error down with it.
  const reload = () => { setError(''); setEpoch(n => n + 1); };

  // The printable report is rendered server-side: the Go backend builds an
  // HTML+CSS document styled with this app's own palette and prints it with a
  // headless Chromium (see internal/report). Fetching it as a blob, rather
  // than linking straight to the URL, is what lets us send the Authorization
  // header the endpoint requires.
  async function downloadPDF() {
    if (!report || downloading) return;
    setDownloading(true);
    try {
      // The PDF endpoint matches the mode, so the downloaded file is the period
      // on screen and not a daily report wearing a different name.
      const typeQ = typeFilter ? `&type=${encodeURIComponent(typeFilter)}` : '';
      const statusQ = `&status=${encodeURIComponent(statusFilter)}`;
      const pdfUrl = mode === 'daily'   ? `${API}/reports/daily.pdf?date=${date}${typeQ}${statusQ}`
                   : mode === 'monthly' ? `${API}/reports/monthly.pdf?month=${month}${typeQ}${statusQ}`
                   :                      `${API}/reports/range.pdf?from=${from}&to=${to}${typeQ}${statusQ}`;
      const res = await fetch(pdfUrl, {
        headers: { 'Authorization': `Bearer ${token}` },
      });
      if (!res.ok) {
        let msg = 'Could not generate the PDF';
        try {
          const body = await res.json();
          if (body.error) msg = typeof body.error === 'string' ? body.error : Object.values(body.error).join(', ');
        } catch {}
        throw new Error(msg);
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      const statusSuffix = statusFilter === 'all' ? '' : `-${statusFilter}`;
      const dlName = mode === 'daily'   ? `LIRA-Daily-Report-${report.date || date}${statusSuffix}.pdf`
                   : mode === 'monthly' ? `LIRA-Monthly-Report-${month}${statusSuffix}.pdf`
                   :                      `LIRA-Report-${from}-to-${to}${statusSuffix}.pdf`;
      a.download = dlName;
      document.body.appendChild(a);
      a.click();
      a.remove();
      // Release the object URL once the download has been handed to the browser.
      setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (ex) {
      setError('Error: ' + ex.message);
    } finally {
      setDownloading(false);
    }
  }

  const fpCount    = report ? (report.summary.false_positives || 0) : 0;
  const realCount  = report ? report.summary.total_issues - fpCount : 0;
  const fpRate     = report && report.summary.total_issues > 0 ? (fpCount / report.summary.total_issues * 100).toFixed(0) : '0';

  /* Theme tokens rather than hex literals, for the reason given above the
     Dashboard's own stat grid: written as literals these pinned the tiles to the
     light palette, so the dark theme left "Resolved" sitting at 3.90:1 against
     body ink's 14.36:1. */
  const summaryCards = report ? [
    {label:'Total issues',     value: report.summary.total_issues,  sub:'Apts + Departments',             color:'var(--blue)'},
    {label:'Resolved',         value: report.summary.resolved,      sub:`${report.summary.resolution_rate.toFixed(0)}% rate`, color:'var(--green)'},
    {label:'Pending',          value: report.summary.pending,       sub:'Still open',                     color:'var(--amber)'},
    {label:'Avg. time',        value: report.summary.avg_minutes.toFixed(1)+'m', sub:'Per issue',         color:'var(--teal)'},
    {label:'Total time spent', value: report.summary.total_minutes+'m', sub:'All issues combined',        color:'var(--blue)'},
    {label:'Fastest fix',      value: report.summary.fastest_minutes+'m', sub:'Quickest resolution',     color:'var(--green)'},
    {label:'Slowest fix',      value: report.summary.slowest_minutes+'m', sub:'Longest resolution',      color:'var(--amber)'},
    {label:'Apt. issues',      value: report.summary.apt_issues,    sub:'Apartment',                     color:'var(--blue)'},
    {label:'Dept. issues',     value: report.summary.dept_issues,   sub:'Department',                    color:'var(--teal)'},
    {label:'False positives',  value: fpCount,                      sub:'Nothing was wrong',              color:'var(--amber)'},
    {label:'False positive rate', value: fpRate+'%',                sub:'Of all issues',                  color:'var(--amber)'},
    {label:'Real issues',      value: realCount,                    sub:'Something was wrong',            color:'var(--green)'},
  ] : [];

  const maxType = report ? Math.max(...Object.values(report.by_type), 1) : 1;
  const maxTech = report ? Math.max(...(report.by_technician||[]).map(t=>t.count), 1) : 1;

  return (
    <div>
      {/* Mode selector. One page, three periods: the page shell, the summary
          cards and the charts are the same for all three, so the only thing that
          changes is the period and the issue lists that come with it. Reusing
          .team-segmented keeps the control identical to the consumables page. */}
      <div className="team-viewbar" style={{marginBottom:12}}>
        <div className="team-segmented" role="radiogroup" aria-label={t.reportModeLabel}>
          <button type="button" role="radio" aria-checked={mode==='daily'}
            className={mode==='daily' ? 'active' : ''} onClick={()=>setMode('daily')}>
            {Ic.calendar}<span>{t.reportModeDaily}</span>
          </button>
          <button type="button" role="radio" aria-checked={mode==='monthly'}
            className={mode==='monthly' ? 'active' : ''} onClick={()=>setMode('monthly')}>
            {Ic.calendar}<span>{t.reportModeMonthly}</span>
          </button>
          <button type="button" role="radio" aria-checked={mode==='range'}
            className={mode==='range' ? 'active' : ''} onClick={()=>setMode('range')}>
            {Ic.calendar}<span>{t.reportModeRange}</span>
          </button>
        </div>
      </div>

      {/* One toolbar. The left cluster swaps by mode; Refresh and Download PDF
          are fixed on the right, so the page does not become three toolbars. */}
      <div style={{display:'flex',alignItems:'center',gap:12,marginBottom:22,flexWrap:'wrap'}}>
        {mode === 'daily' && (
          <div style={{display:'flex',alignItems:'center',gap:8,background:'var(--gray-50)',border:'1px solid var(--gray-200)',borderRadius:'var(--radius-md)',padding:'6px 12px'}}>
            {Ic.calendar}
            <input type="date" value={date} onChange={e=>setDate(e.target.value)}
              aria-label={t.historyPickDate}
              style={{border:'none',outline:'none',fontSize:13,fontFamily:'DM Sans,sans-serif',color:'var(--gray-900)',background:'transparent',colorScheme: theme==='dark' ? 'dark' : 'light'}}/>
          </div>
        )}
        {mode === 'monthly' && (
          <>
            <div style={{display:'flex',alignItems:'center',gap:8,background:'var(--gray-50)',border:'1px solid var(--gray-200)',borderRadius:'var(--radius-md)',padding:'6px 12px'}}>
              {Ic.calendar}
              <input type="month" value={month} onChange={e=>setMonth(e.target.value)}
                aria-label={t.reportPickMonth}
                style={{border:'none',outline:'none',fontSize:13,fontFamily:'DM Sans,sans-serif',color:'var(--gray-900)',background:'transparent',colorScheme: theme==='dark' ? 'dark' : 'light'}}/>
            </div>
            <button className="btn btn-ghost" onClick={()=>setMonth(localISODate(new Date()).slice(0,7))}>{t.reportThisMonth}</button>
          </>
        )}
        {mode === 'range' && (
          <div style={{display:'flex',alignItems:'center',gap:8,flexWrap:'wrap'}}>
            <div style={{display:'flex',alignItems:'center',gap:8,background:'var(--gray-50)',border:'1px solid var(--gray-200)',borderRadius:'var(--radius-md)',padding:'6px 12px'}}>
              {Ic.calendar}
              <input type="date" value={from} onChange={e=>setFrom(e.target.value)}
                aria-label={t.reportRangeFrom}
                style={{border:'none',outline:'none',fontSize:13,fontFamily:'DM Sans,sans-serif',color:'var(--gray-900)',background:'transparent',colorScheme: theme==='dark' ? 'dark' : 'light'}}/>
            </div>
            <span style={{color:'var(--gray-400)',fontSize:13}}>→</span>
            <div style={{display:'flex',alignItems:'center',gap:8,background:'var(--gray-50)',border:'1px solid var(--gray-200)',borderRadius:'var(--radius-md)',padding:'6px 12px'}}>
              {Ic.calendar}
              <input type="date" value={to} onChange={e=>setTo(e.target.value)}
                aria-label={t.reportRangeTo}
                style={{border:'none',outline:'none',fontSize:13,fontFamily:'DM Sans,sans-serif',color:'var(--gray-900)',background:'transparent',colorScheme: theme==='dark' ? 'dark' : 'light'}}/>
            </div>
          </div>
        )}
        {/* Issue-type filter. Empty means "all types", which is the default and the
            only value that makes the by-type chart meaningful; a single type turns
            that chart into one bar, which is honest rather than useful, but the
            rest of the report (summary, by technician) is still worth having for
            one type. */}
        <div ref={typeRef} style={{position:'relative'}}>
          <button type="button"
            onClick={()=>setTypeOpen(o=>!o)}
            aria-haspopup="listbox" aria-expanded={typeOpen}
            aria-label={t.reportTypeFilter}
            style={{display:'flex',alignItems:'center',gap:6,background:'var(--gray-50)',border:'1px solid var(--gray-200)',borderRadius:'var(--radius-md)',padding:'6px 12px',fontSize:13,fontFamily:'DM Sans,sans-serif',color:'var(--gray-900)',cursor:'pointer'}}>
            <span>{typeFilter || t.reportAllTypes}</span>
            <span style={{fontSize:10,color:'var(--gray-400)'}}>▾</span>
          </button>
          {typeOpen && (
            <div style={{position:'absolute',top:'calc(100% + 4px)',left:0,zIndex:20,minWidth:'100%',maxHeight:260,overflowY:'auto',background:'var(--gray-50)',border:'1px solid var(--gray-200)',borderRadius:'var(--radius-md)',boxShadow:'0 4px 16px rgba(0,0,0,0.12)',padding:4}}>
              <button type="button"
                onClick={()=>{setTypeFilter(''); setTypeOpen(false);}}
                style={{display:'block',width:'100%',textAlign:'left',padding:'6px 10px',border:'none',background: typeFilter==='' ? 'var(--blue-light)' : 'transparent',color: typeFilter==='' ? 'var(--blue)' : 'var(--gray-900)',borderRadius:'var(--radius-sm)',cursor:'pointer',fontSize:13,fontFamily:'DM Sans,sans-serif'}}>
                {t.reportAllTypes}
              </button>
              {(issueTypes||[]).map(it => (
                <button type="button" key={it.id}
                  onClick={()=>{setTypeFilter(it.name); setTypeOpen(false);}}
                  style={{display:'block',width:'100%',textAlign:'left',padding:'6px 10px',border:'none',background: typeFilter===it.name ? 'var(--blue-light)' : 'transparent',color: typeFilter===it.name ? 'var(--blue)' : 'var(--gray-900)',borderRadius:'var(--radius-sm)',cursor:'pointer',fontSize:13,fontFamily:'DM Sans,sans-serif'}}>
                  {it.name}
                </button>
              ))}
            </div>
          )}
        </div>
        {/* Issue-status filter. Pending is the open set, so one chip covers
            "open and pending" without inventing a second status in the data. */}
        <div style={{display:'flex',alignItems:'center',gap:6}}>
          <span style={{fontSize:12,color:'var(--gray-500)'}}>{t.reportStatusFilter}</span>
          <div className="filter-chips" role="group" aria-label={t.reportStatusFilter}>
            <button type="button" className={`chip ${statusFilter==='all'?'active':''}`}
              aria-pressed={statusFilter==='all'} onClick={()=>setStatusFilter('all')}>
              {t.reportStatusAll}
            </button>
            <button type="button" className={`chip ${statusFilter==='pending'?'active':''}`}
              aria-pressed={statusFilter==='pending'} onClick={()=>setStatusFilter('pending')}>
              {t.reportStatusPending}
            </button>
            <button type="button" className={`chip ${statusFilter==='ok'?'active':''}`}
              aria-pressed={statusFilter==='ok'} onClick={()=>setStatusFilter('ok')}>
              {t.reportStatusResolved}
            </button>
          </div>
        </div>
        <button className="btn btn-ghost" onClick={reload} disabled={loading} aria-busy={loading}>
          {loading ? '…' : t.reportRefresh}
        </button>
        <button className="btn btn-primary" onClick={downloadPDF} disabled={!report || loading || downloading} aria-busy={downloading}
          style={{marginLeft:'auto'}}>
          {Ic.download} {downloading ? t.generatingPdf : t.reportDownloadPdf}
        </button>
      </div>

      {error && (
        <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,marginBottom:16}}>
          {error}
        </div>
      )}

      {loading && (
        <div style={{textAlign:'center',padding:'60px 0',color:'var(--gray-400)',fontSize:14}}>
          Loading report…
        </div>
      )}

      {report && !loading && (
        <>
          {/* Summary cards */}
          <div style={{display:'grid',gridTemplateColumns:'repeat(3,1fr)',gap:12,marginBottom:22}}>
            {summaryCards.map(c=>(
              <div className="stat-card" key={c.label}>
                <div className="stat-label">{c.label}</div>
                <div className="stat-value" style={{color:c.color}}>{c.value}</div>
                <div className="stat-sub"><span className="stat-dot" style={{background:c.color}}></span>{c.sub}</div>
              </div>
            ))}
          </div>

          {/* Breakdowns */}
          <div className="bottom-grid" style={{marginBottom:22}}>
            <div className="mini-card">
              <div className="mini-card-title">Issues by type</div>
              {Object.keys(report.by_type).length === 0
                ? <p style={{color:'var(--gray-400)',fontSize:13}}>No issues logged.</p>
                : Object.entries(report.by_type).sort((a,b)=>b[1]-a[1]).map(([type,count])=>(
                    <div className="bar-row" key={type}>
                      <div className="bar-label">
                        <span style={{display:'inline-block',width:10,height:10,borderRadius:2,background:TYPE_COLORS[type]||'var(--gray-400)',flexShrink:0}}></span>
                        {type}
                      </div>
                      <div className="bar-track"><div className="bar-fill" style={{width:`${(count/maxType)*100}%`,background:TYPE_COLORS[type]||'var(--gray-400)'}}></div></div>
                      <div className="bar-count">{count}</div>
                    </div>
                  ))
              }
            </div>
            <div className="mini-card">
              <div className="mini-card-title">By technician</div>
              {(report.by_technician||[]).length === 0
                ? <p style={{color:'var(--gray-400)',fontSize:13}}>No technicians.</p>
                : report.by_technician.map(ts=>(
                    <div className="bar-row" key={ts.user_id}>
                      <div className="bar-label"><Avatar name={shortName(ts.name)} idx={ts.avatar_idx} size="sm"/>{shortName(ts.name)}</div>
                      <div className="bar-track"><div className="bar-fill" style={{width:`${(ts.count/maxTech)*100}%`,background:'var(--blue)'}}></div></div>
                      <div className="bar-count">{ts.count}</div>
                    </div>
                  ))
              }
            </div>
          </div>

          {/* Monthly and range reports are aggregates on screen: the summary cards
              and the two charts above are the whole story, and the issue rows live
              in the PDF. Showing them here would mean paging through hundreds of
              rows for a month, which is not reading them. The note says where the
              detail went so the missing tables are not mistaken for a failed load. */}
          {mode !== 'daily' && report.summary.total_issues > 0 && (
            <div className="table-card" style={{marginBottom:20}}>
              <div className="empty-state" style={{padding:'20px 0'}}>
                {Ic.report}
                <p>{t.reportAggregatesOnly}</p>
              </div>
            </div>
          )}

          {/* Apt issues table */}
          {(report.apt_issues||[]).length > 0 && (
            <div className="table-card" style={{marginBottom:20}}>
              <div className="table-header">
                <span className="table-title">{t.aptIssuesTitle} <span style={{fontSize:12,color:'var(--gray-400)',fontWeight:400}}>({report.apt_issues.length})</span></span>
              </div>
              <TableScroll label={t.aptIssuesTitle}>
              <table>
                <thead>
                  <tr><th>Apt.</th><th>{t.date}</th><th>Type</th><th>Problem</th><th>Resolution</th><th>Time</th><th>Status</th><th>Technician</th></tr>
                </thead>
                <tbody>
                  {report.apt_issues.map(i=>(
                    <tr key={i.id} className={i.false_positive ? 'fp-row' : ''}>
                      <td><strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{i.location}</strong></td>
                      <td style={{fontFamily:'DM Mono,monospace',fontSize:12.5,color:'var(--gray-600)',whiteSpace:'nowrap'}}>{reportIssueDate(i, lang)}</td>
                      <td><TypeBadge type={i.type}/></td>
                      <td style={{maxWidth:140,fontSize:13}}>{i.problem}{i.false_positive && <div><span className="fp-badge">{Ic.check} {t.falsePositiveBadge}</span></div>}</td>
                      <td style={{maxWidth:180,fontSize:13,color:'var(--gray-600)'}}>{i.resolution||'—'}</td>
                      <td><IssueTime minutes={i.time_minutes} sla={slaMinutes} t={t}/></td>
                      <td><span className={`badge ${i.status==='Ok'?'badge-ok':'badge-pending'}`}>{i.status}</span></td>
                      <td>
                        <div style={{display:'flex',alignItems:'center',gap:7}}>
                          <Avatar name={shortName(i.logged_by_name)} idx={i.logged_by_idx||0} size="sm"/>
                          <span style={{fontSize:13,color:'var(--gray-600)'}}>{shortName(i.logged_by_name)}</span>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </TableScroll>
            </div>
          )}

          {/* Dept issues table */}
          {(report.dept_issues||[]).length > 0 && (
            <div className="table-card">
              <div className="table-header">
                <span className="table-title">{t.deptIssuesTitle} <span style={{fontSize:12,color:'var(--gray-400)',fontWeight:400}}>({report.dept_issues.length})</span></span>
              </div>
              <TableScroll label={t.deptIssuesTitle}>
              <table>
                <thead>
                  <tr><th>Dept.</th><th>{t.date}</th><th>Type</th><th>Problem</th><th>Resolution</th><th>Time</th><th>Status</th><th>Technician</th></tr>
                </thead>
                <tbody>
                  {report.dept_issues.map(i=>(
                    <tr key={i.id} className={i.false_positive ? 'fp-row' : ''}>
                      <td><strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{i.location}</strong></td>
                      <td style={{fontFamily:'DM Mono,monospace',fontSize:12.5,color:'var(--gray-600)',whiteSpace:'nowrap'}}>{reportIssueDate(i, lang)}</td>
                      <td><TypeBadge type={i.type}/></td>
                      <td style={{maxWidth:140,fontSize:13}}>{i.problem}{i.false_positive && <div><span className="fp-badge">{Ic.check} {t.falsePositiveBadge}</span></div>}</td>
                      <td style={{maxWidth:180,fontSize:13,color:'var(--gray-600)'}}>{i.resolution||'—'}</td>
                      <td><IssueTime minutes={i.time_minutes} sla={slaMinutes} t={t}/></td>
                      <td><span className={`badge ${i.status==='Ok'?'badge-ok':'badge-pending'}`}>{i.status}</span></td>
                      <td>
                        <div style={{display:'flex',alignItems:'center',gap:7}}>
                          <Avatar name={shortName(i.logged_by_name)} idx={i.logged_by_idx||0} size="sm"/>
                          <span style={{fontSize:13,color:'var(--gray-600)'}}>{shortName(i.logged_by_name)}</span>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </TableScroll>
            </div>
          )}

          {/* Both tables came from one request and page together, so there is one
              footer for the pair. Neither table's own count is the count of rows
              the server sent -- each is capped at the same limit independently --
              so the pair is added up from the per-mode totals in the summary,
              which describe the whole day. */}
          <LoadMore list={paged} t={t}
            shown={Math.min(paged.limit, report.summary.apt_issues||0) + Math.min(paged.limit, report.summary.dept_issues||0)}
            total={(report.summary.apt_issues||0) + (report.summary.dept_issues||0)}/>

          {report.summary.total_issues === 0 && (
            <div className="table-card">
              <div className="empty-state">
                {Ic.report}
                <p>
              {mode === 'daily'   && t.reportEmptyDay.replace('{date}', report.date || date)}
              {mode === 'monthly' && t.reportEmptyMonth.replace('{month}', new Date(month + '-01').toLocaleDateString(undefined, {month:'long', year:'numeric'}))}
              {mode === 'range'   && t.reportEmptyRange.replace('{from}', from).replace('{to}', to)}
            </p>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}


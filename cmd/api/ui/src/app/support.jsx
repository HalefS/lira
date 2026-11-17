/* ════════════════ THIRD-PARTY SUPPORT PAGE ════════════════ */
/* How long each external company took. The figures come from
   /v1/support-requests, which computes them from the handovers alone — they are
   never folded into the team's own average resolution time, because "how slow
   is the vendor" and "how fast are we" are different questions. */
const SUPPORT_RANGES = [
  {id:'all',  days:0},
  {id:'7',    days:7},
  {id:'30',   days:30},
  {id:'90',   days:90},
];

/* The window behind the selected chip. Shared by the list and the PDF download
   so the report cannot cover a different period from the one on screen. */
function supportRangeParams(range) {
  const days = SUPPORT_RANGES.find(r=>r.id===range).days;
  const params = new URLSearchParams();
  if (days > 0) {
    // Inclusive of today, so "last 30 days" is 30 days wide rather than 29.
    const from = new Date(); from.setDate(from.getDate() - (days - 1));
    params.set('from', localISODate(from));
    params.set('to', localISODate(new Date()));
  }
  return params;
}

function SupportStatCard({ stats, label, t, isOverall }) {
  const hasData = stats.timed > 0;
  const name = isOverall ? t.supportOverall
            : stats.company === 'telnet' ? t.companyTelnet : t.companyTelefonica;
  return (
    <div className="support-stat-card">
      <div className="support-stat-head">
        <span className="support-stat-name">
          {!isOverall && <span className={`tp-company-dot ${stats.company}`}/>}
          {name}
        </span>
        {isOverall && <span className="badge" style={{background:'var(--gray-100)',color:'var(--gray-500)'}}>{t.supportOverall}</span>}
      </div>

      {hasData ? (
        <div className="support-stat-figures">
          <div>
            <div className="support-figure-label">{t.supportStatAvg}</div>
            <div className="support-figure-value">{formatSupportDuration(stats.avg_minutes)}</div>
          </div>
          <div>
            <div className="support-figure-label">{t.supportStatMedian}</div>
            <div className="support-figure-value">{formatSupportDuration(stats.median_minutes)}</div>
            <div className="support-figure-note">{stats.timed} {t.supportStatTimed}</div>
          </div>
          <div>
            <div className="support-figure-label">{t.supportStatFastest}</div>
            <div className="support-figure-value" style={{color:'var(--green)'}}>{formatSupportDuration(stats.fastest_minutes)}</div>
          </div>
          <div>
            <div className="support-figure-label">{t.supportStatSlowest}</div>
            <div className="support-figure-value" style={{color:'var(--red)'}}>{formatSupportDuration(stats.slowest_minutes)}</div>
          </div>
        </div>
      ) : (
        <div className="support-stat-empty">{t.supportStatNoData}</div>
      )}

      <div className="support-counts">
        <span className="support-count"><b>{stats.total}</b> {t.supportStatHandovers}</span>
        <span className="support-count"><b>{stats.resolved}</b> {t.supportStatResolved}</span>
        {stats.pending > 0 && <span className="support-count" style={{color:'var(--amber)'}}><b style={{color:'var(--amber)'}}>{stats.pending}</b> {t.supportStatPending}</span>}
      </div>
    </div>
  );
}

function SupportPage({ token }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [range, setRange] = useState('all');
  // The printable report is fetched as a blob so the Authorization header can
  // go with it. One notice carries both the "downloaded" confirmation and any
  // error: a click on a button with no visible state leaves nobody wiser.
  const [downloading, setDownloading] = useState(false);
  const [notice, setNotice] = useState(null);

  /* The table pages; the statistics above it do not. Both come from the same
     request, and the server computes the figures over every matching row whatever
     the limit is -- so the headline numbers keep describing the whole period
     while only a pageful of handovers is on screen. */
  const paged = usePagedList(limit => {
    const params = supportRangeParams(range);
    params.set('limit', String(limit));
    return `/support-requests?${params}`;
  }, token, {rowsKey:'requests', filterKey: range});

  const data = paged.res;
  const loading = paged.loading && !data;
  const stats  = data && data.stats;
  const rows   = (data && data.requests) || [];
  // The same window is either a same-day turnaround or a multi-day one, so show
  // the time of day only when every row fits inside a single day.
  const withTime = rows.some(r => {
    if (!r.started_at || !r.resolved_at) return false;
    return new Date(r.started_at).toDateString() === new Date(r.resolved_at).toDateString();
  });

  async function downloadReport() {
    if (downloading) return;
    setDownloading(true);
    setNotice(null);
    try {
      const params = supportRangeParams(range);
      const res = await fetch(`${API}/support-requests/report.pdf?${params}`, {
        headers: { 'Authorization': `Bearer ${token}` },
      });
      if (!res.ok) {
        let msg = t.supportReportError;
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
      const from = params.get('from'), to = params.get('to');
      a.download = `LIRA-Third-Party-Support${from ? `-${from}-to-${to}` : ''}.pdf`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      // Release the object URL once the browser has the download in hand.
      setTimeout(() => URL.revokeObjectURL(url), 10000);
      setNotice({kind:'ok', text:t.supportReportReady});
    } catch (ex) {
      setNotice({kind:'error', text:`${t.supportReportError}: ${ex.message}`});
    } finally {
      setDownloading(false);
    }
  }

  return (
    <>
      <p style={{fontSize:13,color:'var(--gray-400)',margin:'-6px 0 18px',maxWidth:760}}>{t.supportSubtitle}</p>

      <div className="table-card" style={{marginBottom:18}}>
        <div className="table-header">
          <span className="table-title">{t.supportRange}</span>
          <div style={{display:'flex',alignItems:'center',gap:12,flexWrap:'wrap'}}>
            <div className="filter-chips" role="group" aria-label={t.supportRange}>
              {SUPPORT_RANGES.map(r=>(
                <button type="button" key={r.id} className={`chip ${range===r.id?'active':''}`}
                  onClick={()=>{setRange(r.id); setNotice(null);}}>
                  {t[`supportRange${r.id==='all'?'All':r.id}`]}
                </button>
              ))}
            </div>
            <button type="button" className="btn btn-primary" onClick={downloadReport}
              disabled={downloading} aria-busy={downloading}>
              {Ic.download} {downloading ? t.generatingPdf : t.supportDownloadPdf}
            </button>
          </div>
        </div>
        {/* Always in the DOM so a screen reader is listening before the click.
            The same message is visible, so the confirmation is not exclusive. */}
        <div aria-live="polite" aria-atomic="true" style={{minHeight:0,padding:notice?'8px 16px 0':0,fontSize:12.5,color:notice&&notice.kind==='error'?'var(--red)':'var(--green)'}}>
          {notice && <span>{notice.text}</span>}
        </div>
      </div>

      {stats && (
        <>
          <div className="support-grid">
            {stats.by_company.map(s => <SupportStatCard key={s.company} stats={s} t={t}/>)}
          </div>
          <SupportStatCard stats={stats.overall} t={t} isOverall/>
        </>
      )}

      <p className="support-note" style={{marginTop:18}}>{t.supportExcluded}</p>

      <div className="table-card">
        <div className="table-header">
          <span className="table-title">{t.supportListTitle}</span>
          <span style={{fontSize:12,color:'var(--gray-400)'}}>{rows.length}</span>
        </div>
        {loading ? (
          <div className="empty-state"><p>…</p></div>
        ) : rows.length === 0 ? (
          <div className="empty-state">{Ic.clock}<p>{t.supportListEmpty}</p></div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>{t.supportListColumnIssue}</th><th>{t.supportListColumnCompany}</th>
                <th>{t.supportListColumnReference}</th><th>{t.supportListColumnReported}</th>
                <th>{t.supportListColumnSolution}</th><th>{t.supportListColumnDuration}</th>
                <th>{t.supportListColumnStatus}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(r=>(
                <tr key={r.id}>
                  <td>
                    <strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{r.issue_location}</strong>
                    <div style={{fontSize:12.5,color:'var(--gray-500)',marginTop:2}}>{r.issue_problem}</div>
                  </td>
                  <td>
                    <span className={`tp-strip-item ${r.company}`} style={{fontSize:11}}>
                      {r.company==='telnet' ? t.companyTelnet : t.companyTelefonica}
                    </span>
                  </td>
                  <td style={{fontSize:12.5,color:'var(--gray-600)'}}>{r.reference || '—'}</td>
                  <td style={{fontSize:12.5,color:'var(--gray-500)',whiteSpace:'nowrap'}}>
                    {formatSupportMoment(r.started_at, withTime) || '—'}
                  </td>
                  <td style={{fontSize:12.5,color:'var(--gray-500)',whiteSpace:'nowrap'}}>
                    {formatSupportMoment(r.resolved_at, withTime) || '—'}
                  </td>
                  <td>
                    <span className={`support-dur ${r.duration_minutes==null ? 'pending' : ''}`}>
                      {formatSupportDuration(r.duration_minutes) || '—'}
                    </span>
                  </td>
                  <td>
                    <span className={`badge ${r.status==='solved'?'badge-ok':'badge-pending'}`}>
                      {r.status==='solved' ? t.supportSolved : t.supportPending}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <LoadMore list={paged} t={t}/>
      </div>
    </>
  );
}

/* Every TV swap in one list, for the overview the per-issue strips cannot give:
   which rooms have been borrowing sets from each other, and whether the same pair
   keeps recurring (which usually means one room's set is failing for good). */
function TVSwapsPage({ token }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [range, setRange] = useState('all');
  const [query, setQuery] = useState('');
  // Bumped by the retry button under a failed request, in the same way the issue
  // tables do it. The range alone is not enough to re-run a fetch that failed.
  const [retry, setRetry] = useState(0);

  /* The swap list pages from the server. The search box below still narrows what
     has been loaded rather than going to the server, which is a deliberate
     difference from the issues table: the terms here are room numbers and short
     notes typed by a technician looking at one screen, and the endpoint has no
     search of its own to send them to. So "load more" is what widens it, and the
     footer says how much is loaded out of how much exists. */
  const paged = usePagedList(limit => {
    const days = SUPPORT_RANGES.find(r=>r.id===range).days;
    const params = new URLSearchParams({limit: String(limit)});
    if (days > 0) {
      // Inclusive of today, so "last 30 days" is 30 days wide rather than 29.
      const from = new Date(); from.setDate(from.getDate() - (days - 1));
      params.set('from', localISODate(from));
      params.set('to', localISODate(new Date()));
    }
    return `/tv-swaps?${params}`;
  }, token, {rowsKey:'swaps', filterKey: `${range}|${retry}`});

  const swaps = paged.rows || [];
  // Gated on the error as well as the rows: a failed request also leaves rows
  // null, so asking only for rows drew "no swaps recorded" for a request that
  // had already come back broken -- a page reporting an absence it never asked
  // about. Same reasoning as the LCU list and the issue table.
  const loading = paged.loading && !paged.rows && !paged.error;

  const q = query.trim().toLowerCase();
  const shown = q
    ? swaps.filter(s => [s.from_room, s.to_room, s.notes, s.issue_problem, s.issue_type]
        .some(v => String(v||'').toLowerCase().includes(q)))
    : swaps;

  // Counted over the whole range, not over `shown`. Tying the search box's
  // visibility to the filtered list would make it vanish the moment it
  // narrowed the results, stranding the user in the filtered view with no way
  // to widen it again.
  const allRooms = useMemo(() => {
    const set = new Set();
    for (const s of swaps) { set.add(s.from_room); set.add(s.to_room); }
    return set.size;
  }, [swaps]);

  // A room that borrowed more than it lent is the one worth looking at: it is
  // living off a set that belongs elsewhere. Counted from the destination side
  // so "1301 <- 1204" credits 1204 as the lender.
  const tally = useMemo(() => {
    const borrowed = new Map(), lent = new Map();
    for (const s of shown) {
      borrowed.set(s.to_room,   (borrowed.get(s.to_room)   || 0) + 1);
      lent.set(s.from_room,     (lent.get(s.from_room)     || 0) + 1);
    }
    return {borrowed, lent};
  }, [shown]);

  const rooms = useMemo(() => {
    const set = new Set();
    for (const s of shown) { set.add(s.from_room); set.add(s.to_room); }
    return [...set].sort((a,b)=>a.localeCompare(b, undefined, {numeric:true}));
  }, [shown]);

  return (
    <>
      <p style={{fontSize:13,color:'var(--gray-400)',margin:'-6px 0 18px',maxWidth:760}}>{t.tvSwapsSubtitle}</p>

      <div className="table-card" style={{marginBottom:18}}>
        <div className="table-header">
          <span className="table-title">{t.supportRange}</span>
          <div className="filter-chips">
            {SUPPORT_RANGES.map(r=>(
              <button key={r.id} className={`chip ${range===r.id?'active':''}`} onClick={()=>setRange(r.id)}>
                {t[`supportRange${r.id==='all'?'All':r.id}`]}
              </button>
            ))}
          </div>
        </div>
      </div>

      {rooms.length > 0 && (
        <div className="analytics-grid" style={{marginBottom:18}}>
          <div className="stat-card">
            <div className="stat-label" style={{marginBottom:4}}>{t.tvSwapsStatTotal}</div>
            <div className="stat-value">{shown.length}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label" style={{marginBottom:4}}>{t.tvSwapsStatRooms}</div>
            <div className="stat-value">{rooms.length}</div>
          </div>
        </div>
      )}

      {/* The search box lives outside the table so it survives the results
          narrowing to nothing — otherwise a search that matched nothing would
          remove the only control that could undo it. */}
      {!loading && allRooms > 8 && (
        <div className="table-card" style={{marginBottom:18}}>
          <div className="search-wrap" style={{padding:'14px 20px 0'}}>
            {Ic.search}
            <input aria-label={t.tvSwapsSearch} placeholder={t.tvSwapsSearch} value={query} onChange={e=>setQuery(e.target.value)}/>
          </div>
        </div>
      )}

      {paged.error ? (
        <div className="table-card">
          <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,margin:'14px 20px',display:'flex',alignItems:'center',gap:10}}>
            <span style={{flex:1}}>{paged.error}</span>
            <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
              onClick={()=>setRetry(n => n + 1)}>{t.tryAgain}</button>
          </div>
        </div>
      ) : loading ? (
        <div className="empty-state"><p>{t.analyticsLoading}</p></div>
      ) : shown.length === 0 ? (
        <div className="empty-state">{Ic.tv}<p>{q ? t.tvSwapsNoMatch : t.tvSwapsEmpty}</p></div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>{t.date}</th>
                <th>{t.tvSwapFrom}</th>
                <th>{t.tvSwapTo}</th>
                <th>{t.tvSwapsColIssue}</th>
                <th>{t.tvSwapNotes}</th>
                <th>{t.loggedBy}</th>
              </tr>
            </thead>
            <tbody>
              {shown.map(s=>{
                const borrowed = tally.borrowed.get(s.to_room) || 0;
                const lent     = tally.lent.get(s.from_room)     || 0;
                return (
                  <tr key={s.id}>
                    <td style={{fontSize:12,color:'var(--gray-400)',whiteSpace:'nowrap'}}>
                      {new Date(s.created_at).toLocaleString()}
                    </td>
                    <td>
                      <div style={{display:'flex',alignItems:'center',gap:7}}>
                        <span style={{color:'var(--gray-400)',fontSize:11}}>{Ic.tv}</span>
                        <strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{s.from_room}</strong>
                        {/* A room that has lent more often than it borrowed is
                            the one worth looking at: its own sets keep going
                            somewhere else. */}
                        {lent > 1 && (
                          <span className="badge" style={{background:'var(--amber-light)',color:'var(--amber)',fontSize:10,padding:'1px 7px'}}>
                            {lent} {t.tvSwapsLent}
                          </span>
                        )}
                      </div>
                    </td>
                    <td>
                      <div style={{display:'flex',alignItems:'center',gap:7}}>
                        <span style={{color:'var(--gray-300)',fontSize:13}}>{'\u2192'}</span>
                        <strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{s.to_room}</strong>
                        {borrowed > 1 && (
                          <span className="badge" style={{background:'var(--amber-light)',color:'var(--amber)',fontSize:10,padding:'1px 7px'}}>
                            {borrowed} {t.tvSwapsBorrowed}
                          </span>
                        )}
                      </div>
                    </td>
                    <td style={{maxWidth:180}}>
                      <div style={{display:'flex',alignItems:'center',gap:7}}>
                        <TypeBadge type={s.issue_type}/>
                        <strong style={{fontFamily:'DM Mono,monospace',fontSize:12.5}}>{s.issue_location}</strong>
                      </div>
                      <div style={{fontSize:12.5,color:'var(--gray-500)',marginTop:2}}>{s.issue_problem}</div>
                    </td>
                    <td style={{fontSize:12.5,color:'var(--gray-500)'}}>{s.notes || '—'}</td>
                    <td style={{fontSize:12.5,color:'var(--gray-600)'}}>{shortName(s.logged_by_name) || '—'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <LoadMore list={paged} t={t}/>
    </>
  );
}

/* The five statuses, in the order the summary tiles show them. Kept as data
   rather than five hand-written tiles so the filter chips, the labels and the
   counts cannot drift apart. */

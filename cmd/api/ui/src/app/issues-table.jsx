/* ════════════════ ISSUES TABLE ════════════════ */
// The one place that decides whether a resolution time has breached the SLA.
//
// Four tables show a resolution time -- both issue lists, the history table, the
// daily report and the profile -- and a component rather than a helper returning
// a style object is what keeps the comparison and the red from drifting apart
// between them. With no SLA configured it renders exactly what these tables
// rendered before one existed, so the feature is invisible until it is switched on.
//
// sla is nullable and that is the whole off switch: null means no SLA, so nothing
// is ever flagged. Zero could not mean the same thing -- it would mark every issue
// that took any time at all, including a three-minute fix.
function IssueTime({ minutes, sla, t }) {
  const m = Number(minutes) || 0;
  const base = {fontFamily:'DM Mono,monospace', fontSize:13};
  if (sla == null || m <= sla) return <span style={base}>{m}m</span>;
  return (
    <span className="sla-breach" style={base}
      title={t.slaBreachedTitle.replace('{minutes}', sla)}>
      {m}m
    </span>
  );
}

/* `onFailure` / `peerError` / `isPrimary` exist only for the History page, which
   stacks two of these. They share one endpoint, so a dead /issues fails both at
   once and printed the same red banner twice. The parent collects both failures
   through onFailure and passes each table the other's, so the first table to fail
   draws the banner and the second stays quiet -- and a table that fails alone
   still draws its own, rather than being silenced by a peer it is not failing
   with. `isPrimary` is what breaks the tie when both have failed.

   `retrySignal` is the matching half: the parent increments it so one Try again
   re-asks for both tables rather than only the one it was clicked in. */
function IssuesTable({ token, issueTypes, tableMode, date='', epoch=0, currentUser, newIssueId, slaMinutes, onDelete, onEdit, onFailure, peerError, isPrimary, retrySignal=0, onRetryAll, emptyText=null }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  // `null` means "no type filter", rather than the translated word for "All".
  // Comparing against t.all left the table empty in any language whose word
  // for "All" isn't literally "All" (Portuguese: "Todas"), and an issue type
  // could also be *named* All/Todas, which the old check couldn't tell apart
  // from the catch-all chip.
  const [filter, setFilter] = useState(null);
  const [search, setSearch] = useState('');
  // Bumped by the retry button under a failed request. Separate from the `epoch`
  // prop -- that one is the page telling this table the world moved on, and is not
  // something a button here should be advancing on the app's behalf.
  const [retry, setRetry] = useState(0);

  /* The search box talks to the server rather than filtering the rows in hand.
     It used to narrow an array that held every issue ever logged, which is why
     it could be a plain input handler. Now that only a page of rows is in hand,
     filtering locally would mean searching twenty issues and calling it the whole
     list -- so the term goes to the endpoint, which already knew how to match it.
     Debounced, because each keystroke otherwise becomes a request. */
  const [query, setQuery] = useState('');
  useEffect(() => {
    const id = setTimeout(() => setQuery(search.trim()), 250);
    return () => clearTimeout(id);
  }, [search]);

  // `date` is how the History page narrows to a single day. It goes to the server
  // rather than being applied to whatever arrived, for the same reason the search
  // does: a day filter applied to a loaded page would report a day with nothing
  // in it while the server was holding the rows.
  const list = usePagedList(limit => {
    const qs = new URLSearchParams({mode: tableMode, limit: String(limit)});
    if (filter) qs.set('type', filter);
    if (query) qs.set('search', query);
    if (date) qs.set('date', date);
    return `/issues?${qs}`;
  }, token, {rowsKey:'issues', map:mapIssue, filterKey: `${tableMode}|${filter}|${query}|${date}|${epoch}|${retry}|${retrySignal}`});

  const issues = list.rows || [];

  // Reported up to the History page. A ref-and-callback rather than a dependency
  // so a parent passing an inline arrow does not restart the fetch.
  const onFailureRef = useRef(onFailure);
  onFailureRef.current = onFailure;
  useEffect(() => {
    if (onFailureRef.current) onFailureRef.current(tableMode, list.error || null);
  }, [tableMode, list.error]);
  // The chips come from the type catalog rather than from the rows on screen: with
  // only the loaded page in hand, a type whose issues are all further down would
  // have no chip at all, and picking it was impossible.
  const chips = [{label:t.all, value:null}, ...issueTypes.map(ty=>({label:ty.name, value:ty.name}))];

  const canAct = issue => currentUser.role==='manager' || issue.loggedBy===currentUser.id;

  // Clicking anywhere in a row opens it for editing, but only for the user who
  // logged it (or a manager) — the same rule the edit button and the API enforce.
  function openFromRow(e, issue) {
    // A click that ends with text selected is someone trying to copy
    // something out of the row, not someone trying to edit the issue.
    if (window.getSelection()?.toString()) return;
    onEdit(issue, tableMode);
  }

  return (
    <div className="table-card">
      <div className="table-header">
        <span className="table-title">{tableMode==='apt' ? t.aptIssuesTitle : t.deptIssuesTitle}</span>
        <div className="table-actions">
          <div className="filter-chips">
            {chips.map(c=><button key={c.value ?? '__all__'} className={`chip ${filter===c.value?'active':''}`} aria-pressed={filter===c.value} onClick={()=>setFilter(c.value)}>{c.label}</button>)}
          </div>
          <div className="search-wrap">
            {Ic.search}
            <input aria-label={tableMode==='apt' ? t.searchApt : t.searchDept}
              placeholder={tableMode==='apt' ? t.searchApt : t.searchDept} value={search} onChange={e=>setSearch(e.target.value)}/>
          </div>
        </div>
      </div>
      {/* The request failed. Checked before rows, because a failed request also
          leaves rows null -- asking for rows alone meant a dead endpoint rendered
          "Loading analytics…" here for as long as the page was open. */}
      {list.error ? (
        /* Kept quiet when this table's peer has failed too and it is not the one
           that owns the banner -- same failure, same message, same retry, printed
           twice on one screen. */
        (peerError && !isPrimary) ? null :
        <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,marginBottom:16,display:'flex',alignItems:'center',gap:10}}>
          <span style={{flex:1}}>{list.error}</span>
          <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
            onClick={() => { setRetry(n => n + 1); if (onRetryAll) onRetryAll(); }}>{t.tryAgain}</button>
        </div>
      ) : !list.rows ? (
        <div className="empty-state">{Ic.list}<p>{t.analyticsLoading}</p></div>
      ) : issues.length===0 ? (
        <div className="empty-state">{Ic.list}<p>{emptyText || t.noMatch}</p></div>
      ) : (
        /* Scrolls sideways rather than being clipped by the card. Eight columns
           will not fit a laptop window, and .table-card is overflow:hidden to clip
           its own corners -- so without this the last column and the row's edit and
           delete buttons sit past the card's right edge with no way to reach them,
           and a technician on a narrow screen cannot edit or delete an issue at all.
           TableScroll is the shared wrapper: it also makes the strip a named,
           focusable region, which is what lets a keyboard reach the columns that
           are scrolled out of view. */
        <TableScroll label={tableMode==='apt' ? t.aptIssuesTitle : t.deptIssuesTitle}>
        <table>
          <thead>
            <tr>
              <th>{tableMode==='apt' ? t.apt : t.dept}</th>
              <th>{t.type}</th><th>{t.problem}</th><th>{t.resolution}</th><th>{t.time}</th><th>{t.status}</th><th>{t.loggedBy}</th><th></th>
            </tr>
          </thead>
          <tbody>
            {issues.map(issue=>(
              <tr key={issue.id}
                  className={`${issue.id===newIssueId ? 'row-enter' : ''}${canAct(issue) ? ' row-clickable' : ''}`}
                  onClick={canAct(issue) ? e=>openFromRow(e,issue) : undefined}
                  title={canAct(issue) ? t.editIssue : undefined}>
                <td><strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{issue.apt||issue.dept}</strong></td>
                <td><TypeBadge type={issue.type}/></td>
                <td style={{maxWidth:150}}>{issue.problem}{issue.falsePositive && <div><span className="fp-badge">{Ic.check} {t.falsePositiveBadge}</span></div>}<PhotoStrip photos={issue.attachments} t={t}/><TVSwapStrip swaps={issue.tvSwaps} t={t}/><ConsumableStrip items={issue.consumables}/><SupportRequestStrip requests={issue.supportRequests} t={t}/></td>
                <td style={{maxWidth:190,color:'var(--gray-600)',fontSize:13}}>{issue.resolution}</td>
                <td><IssueTime minutes={issue.time} sla={slaMinutes} t={t}/></td>
                <td><span className={`badge ${issue.status==='Ok'?'badge-ok':'badge-pending'} ${issue.id===newIssueId ? 'status-highlight' : ''}`}>{issue.status}</span></td>
                <td>
                  <div style={{display:'flex',alignItems:'center',gap:7}}>
                    <Avatar name={shortName(issue.loggedByName)} idx={issue.loggedByIdx??0} size="sm"/>
                    <span style={{fontSize:13,color:'var(--gray-600)'}}>{shortName(issue.loggedByName)}</span>
                    {issue.loggedBy===currentUser.id && (
                      <span className="badge" style={{background:'var(--blue-light)',color:'var(--blue)',fontSize:10,padding:'1px 6px'}}>{t.you}</span>
                    )}
                  </div>
                </td>
                <td>
                  <div className="action-cell">
                    {canAct(issue) ? (
                      <>
                        {/* stopPropagation: these sit inside a clickable row, and
                            without it a click on the bin would delete the issue
                            and then open it for editing. */}
                        <button className="icon-btn" title={t.saveChanges} onClick={e=>{e.stopPropagation();onEdit(issue,tableMode);}}>{Ic.edit}</button>
                        <button className="icon-btn danger" title={t.toastDeleted} onClick={e=>{e.stopPropagation();onDelete(issue,tableMode);}}>{Ic.trash}</button>
                      </>
                    ) : (
                      <span style={{fontSize:11,color:'var(--gray-300)',padding:'0 8px',userSelect:'none'}} title="Only the owner or a manager can edit this">—</span>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </TableScroll>
      )}
      <LoadMore list={list} t={t}/>
    </div>
  );
}


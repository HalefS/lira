/* ════════════════ CONSUMABLES PAGE ════════════════ */
/* Lets a manager put stock in, take it out, or correct it to a counted figure.
   The resulting count is previewed live, and a removal that would overdraw the
   item is pointed out before saving — the server allows a negative count, but
   doing it by accident is not what someone reaching for "remove" means. */
function StockAdjustDialog({ item, onCancel, onSave, saving, t }) {
  const [mode, setMode] = useState('add');   // add | remove | set
  const [value, setValue] = useState('');
  const [note, setNote] = useState('');

  // Escape closes, but not while a save is in flight: the dialog owns the
  // message that will say whether it worked, so it cannot be dismissed first.
  const panelRef = useDialog({onClose: saving ? null : onCancel});
  const n = Number(value);
  const valid = value !== '' && Number.isFinite(n) && (mode === 'set' ? n >= 0 : n > 0);
  const result = !valid ? null : mode === 'set' ? n : mode === 'add' ? item.stock + n : item.stock - n;
  const error = !valid ? null
    : mode === 'set'    ? (n === item.stock ? t.stockNoChange : null)
    : mode === 'remove' ? (n > item.stock ? t.stockWouldGoNegative : null)
    : null;

  function submit() {
    if (!valid || error) return;
    if (mode === 'set')   onSave({set_to: n, note});
    else if (mode === 'add') onSave({change: n, note});
    else                  onSave({change: -n, note});
  }

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && !saving && onCancel()}>
      <div className="modal" style={{maxWidth:420}} ref={panelRef} role="dialog" aria-modal="true" aria-label={t.adjustStock}>
        <div className="modal-header">
          <div style={{display:'flex',alignItems:'center',gap:10}}>
            <ConsumableIcon name={item.icon} size={30}/>
            <div>
              <h2>{t.adjustStock}</h2>
              <div className="picker-sub">{item.name}</div>
            </div>
          </div>
          <button type="button" className="icon-btn" onClick={onCancel} disabled={saving}>{Ic.x}</button>
        </div>
        <div className="modal-body">
          <div className="form-group">
            {/* A caption for the three mode buttons, not a label: there is no one control
                behind it. The group role is what names it. */}
            <div className="form-group-h" id="stock-mode-h">{t.stockHow}</div>
            <div className="filter-chips" style={{marginTop:2}} role="group" aria-labelledby="stock-mode-h">
              {['add','remove','set'].map(m=>(
                <button key={m} className={`chip ${mode===m?'active':''}`} aria-pressed={mode===m}
                  onClick={()=>{setMode(m);setValue('');}}>
                  {t['stockMode_'+m]}
                </button>
              ))}
            </div>
          </div>
          <div className="form-group">
            <label htmlFor="stock-qty">{mode === 'set' ? t.stockCountedAs : t.stockHowMany}</label>
            <input id="stock-qty" className="form-control" type="number" inputMode="numeric" value={value} autoFocus
              onChange={e=>setValue(e.target.value)} onKeyDown={e=>e.key==='Enter' && submit()}/>
          </div>
          <div className="form-group">
            <label htmlFor="stock-note">{t.stockNote}</label>
            <input id="stock-note" className="form-control" placeholder={t.stockNotePlaceholder} value={note} maxLength={200}
              onChange={e=>setNote(e.target.value)} onKeyDown={e=>e.key==='Enter' && submit()}/>
          </div>
          {error && <div style={{fontSize:12.5,color:'var(--amber)'}}>{error}</div>}
          {valid && !error && (
            <div style={{fontSize:12.5,color:'var(--gray-400)'}}>
              {t.inStock}: <strong style={{color:'var(--gray-900)'}}>{item.stock}</strong>
              {' → '}
              <strong style={{color: result < 0 ? 'var(--red)' : 'var(--gray-900)'}}>{result}</strong>
            </div>
          )}
        </div>
        <div className="modal-footer">
          <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={saving}>{t.cancel}</button>
          <button type="button" className="btn btn-primary" onClick={submit} disabled={!valid || !!error || saving}>
            {saving ? t.analyticsLoading : t.saveChanges}
          </button>
        </div>
      </div>
    </div>
  );
}

/*
  Two views over the same data. "Log" is the running list of every recorded
  use, filterable by date, together with what is in stock right now. "Weekly
  report" is the Monday-to-Sunday roll-up — what was used, where, on which
  issue and by whom — with a PDF download.
*/
/* One of the two consumable usage tables (apartments / departments).
   At module scope on purpose: nested inside ConsumablesPage it would be a new
   function on every render, so both tables would be torn down and rebuilt
   whenever anything else on the page changed — typing in the item editor
   included. Reads its strings from LangCtx, like every other table here. */
function EntryTable({ rows, locationHeader }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  if (!rows.length) return null;
  return (
    <div className="table-card" style={{marginBottom:20}}>
      <div className="table-header">
        <span className="table-title">{locationHeader} <span style={{fontSize:12,color:'var(--gray-400)',fontWeight:400}}>({rows.length})</span></span>
      </div>
      <table>
        <thead>
          <tr>
            <th>{t.date}</th><th>{t.item}</th><th>{t.qty}</th>
            <th>{t.location}</th><th>{t.issueType}</th><th>{t.issues}</th><th>{t.loggedBy}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(c=>(
            <tr key={c.id}>
              <td style={{fontSize:12,color:'var(--gray-400)',whiteSpace:'nowrap'}}>
                {new Date(c.created_at).toLocaleDateString(lang==='pt'?'pt-PT':'en-GB',{day:'2-digit',month:'short'})}
                {' '}{new Date(c.created_at).toLocaleTimeString(lang==='pt'?'pt-PT':'en-GB',{hour:'2-digit',minute:'2-digit'})}
              </td>
              <td>
                <div style={{display:'flex',alignItems:'center',gap:9}}>
                  <ConsumableIcon name={c.icon} size={26}/>
                  <span style={{fontWeight:500}}>{c.item}</span>
                </div>
              </td>
              <td style={{fontFamily:'DM Mono,monospace',fontSize:13,textAlign:'right'}}>×{c.quantity}</td>
              <td style={{fontFamily: c.mode==='apt' ? 'DM Mono,monospace' : 'DM Sans,sans-serif', fontSize:13}}>{c.location}</td>
              <td><TypeBadge type={c.issue_type}/></td>
              <td style={{fontSize:12,color:'var(--gray-600)',maxWidth:220}}>
                <div style={{overflow:'hidden',textOverflow:'ellipsis',whiteSpace:'nowrap'}} title={c.issue_problem}>
                  #{c.issue_id} {c.issue_problem}
                </div>
              </td>
              <td>
                <div style={{display:'flex',alignItems:'center',gap:7}}>
                  <Avatar name={shortName(c.logged_by_name)} idx={c.logged_by_idx||0} size="sm"/>
                  <span style={{fontSize:13,color:'var(--gray-600)'}}>{shortName(c.logged_by_name)}</span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ConsumablesPage({ token, currentUser, onCatalogUpdate, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const todayStr = localISODate(new Date());
  const [view, setView] = useState('log');
  // Bumped by the retry button under a failed usage request, as on the issue
  // tables. The date alone cannot re-run a fetch that already failed.
  const [retry, setRetry] = useState(0);

  // ── Log view ──
  const [date, setDate] = useState('');
  /* The usage log pages -- it grows by one row every time anything is used on
     any issue, forever. The per-item running totals above it do not: the server
     computes those over every recorded use regardless of the limit, so the
     figures do not quietly shrink as more rows are loaded under them. */
  const usage = usePagedList(limit => {
    const qs = new URLSearchParams({limit: String(limit)});
    if (date) qs.set('date', date);
    return `/consumables?${qs}`;
  }, token, {rowsKey:'consumables', filterKey: `${date}|${view === 'log' ? 'log' : 'other'}|${retry}`});

  const items   = usage.rows || [];
  const summary = (usage.res && usage.res.summary) || [];
  // Gated on the error too. rows is null after a failure as well as while a
  // request is in flight, so the loading branch alone claimed an empty log for
  // an endpoint that had just answered with an error.
  const loading = usage.loading && !usage.rows && !usage.error;
  // Inventory state. The catalog is fetched here as well as from the issue
  // form, because this page is where stock is corrected and explained.
  const [catalog, setCatalog] = useState([]);
  const [adjusting, setAdjusting] = useState(null);
  const [saving, setSaving] = useState(false);
  const [historyFor, setHistoryFor] = useState(null);
  const [movements, setMovements] = useState([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const canManage = !!(currentUser && currentUser.role === 'manager');

  // The issue form's consumable picker is fed from the catalog the app loaded
  // at start-up, which would otherwise still show the count from before any
  // correction made here. Writing every change through to it means a manager
  // who fixes a count is immediately followed by a technician seeing the right
  // number, without anyone reloading.
  const publish = (listOrUpdater) => {
    setCatalog(listOrUpdater);
    if (onCatalogUpdate) onCatalogUpdate(listOrUpdater);
  };
  const replaceItem = updated => publish(list => list.map(ci => ci.id === updated.id ? updated : ci));

  // In full, not a page: this is a catalog rather than a log, and the picker in the
  // issue form has to be able to offer every item in it. The inventory table
  // further down is the same list, and it is where the pagination is worth having.
  const loadCatalog = () =>
    apiFetch('/consumable-items?limit=1000', {token}).then(res => publish(res.consumable_items || []));

  useEffect(() => {
    if (view !== 'log') return;
    loadCatalog().catch(() => {});
  }, [view, token]);

  // ── Consumables report view ──
  // Both periods produce the same report shape; only the anchor and the
  // endpoint change. A week anchor is any day inside that week, a month anchor
  // is "YYYY-MM", and the server reports the resolved range back.
  const [period, setPeriod] = useState('week');
  const [anchor, setAnchor] = useState(todayStr);
  const [month, setMonth] = useState(todayStr.slice(0, 7));
  const [report, setReport] = useState(null);
  const [rLoading, setRLoading] = useState(false);
  const [rError, setRError] = useState('');
  const [downloading, setDownloading] = useState(false);
  const isMonthly = period === 'month';
  const periodKey = isMonthly ? month : anchor;

  useEffect(() => {
    if (view !== 'report') return;
    let cancelled = false;
    setRLoading(true); setRError(''); setReport(null);
    const url = isMonthly
      ? `/reports/consumables/monthly?month=${month}`
      : `/reports/consumables/weekly?week=${anchor}`;
    apiFetch(url, {token})
      .then(res => { if (!cancelled) setReport(res.report || null); })
      .catch(ex => { if (!cancelled) setRError(ex.message); })
      .finally(() => { if (!cancelled) setRLoading(false); });
    return () => { cancelled = true; };
  }, [period, periodKey, token, view]);

  function shiftPeriod(steps) {
    if (!isMonthly) {
      const d = new Date(anchor + 'T00:00:00');
      d.setDate(d.getDate() + steps * 7);
      setAnchor(localISODate(d));
      return;
    }
    // Stepping by whole months from the first avoids the 31st rolling a month
    // over twice when moving from a long month to a short one.
    const [y, m] = month.split('-').map(Number);
    const d = new Date(y, m - 1 + steps, 1);
    setMonth(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`);
  }

  function currentPeriod() {
    const now = new Date();
    if (isMonthly) setMonth(`${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`);
    else setAnchor(localISODate(now));
  }

  // Downloading goes through fetch rather than a plain link so the
  // Authorization header reaches the endpoint.
  async function downloadReportPDF() {
    if (!report || downloading) return;
    setDownloading(true);
    try {
      const url = isMonthly
        ? `/reports/consumables/monthly.pdf?month=${month}`
        : `/reports/consumables/weekly.pdf?week=${anchor}`;
      const res = await fetch(`${API}${url}`, {
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
      const urlDownload = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = urlDownload;
      a.download = isMonthly
        ? `LIRA-Consumables-Month-${month}.pdf`
        : `LIRA-Consumables-Week-${report.from_date}-to-${report.to_date}.pdf`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(urlDownload), 10000);
    } catch (ex) {
      setRError('Error: ' + ex.message);
    } finally {
      setDownloading(false);
    }
  }

  // A bar never reads as zero-width, so a day with one unit is still a day.
  const dayMax = report ? Math.max(1, ...(report.by_day || []).map(d => d.quantity)) : 1;
  // One bar per day of the period, so a quiet day shows as quiet rather than
  // simply missing. The server sends back both bounds, so the same code fills
  // seven slots for a week and 28-31 for a month.
  const dayCells = report ? (() => {
    const byDate = new Map((report.by_day || []).map(d => [d.date, d.quantity]));
    const out = [];
    const d = new Date(report.from_date + 'T00:00:00');
    while (localISODate(d) <= report.to_date) {
      const key = localISODate(d);
      const qty = byDate.get(key) || 0;
      out.push({
        key, qty,
        dow: d.toLocaleDateString(lang === 'pt' ? 'pt-PT' : 'en-GB', { weekday: 'short' }),
        dom: d.getDate(),
        height: qty === 0 ? 2 : Math.max(6, (qty / dayMax) * 100),
      });
      d.setDate(d.getDate() + 1);
    }
    return out;
  })() : [];

  const itemMax    = report ? Math.max(1, ...(report.by_item || []).map(x => x.quantity)) : 1;
  const locMax     = report ? Math.max(1, ...(report.by_location || []).map(x => x.quantity)) : 1;
  const techMax    = report ? Math.max(1, ...(report.by_technician || []).map(x => x.quantity)) : 1;
  // A busy month can list dozens of places. The column keeps its shape by
  // showing the top eight and folding the rest into a single count.
  const locLimit   = 8;
  const locRows    = report ? (report.by_location || []) : [];
  const aptRows    = report ? (report.entries || []).filter(e => e.mode === 'apt') : [];
  const deptRows   = report ? (report.entries || []).filter(e => e.mode !== 'apt') : [];

  const statCards = report ? [
    { label: t.item + 's',       value: report.total_quantity, sub: t.weekTotalUnits,   color: 'var(--blue)' },
    { label: t.records,          value: report.total_records,  sub: t.weekTotalRecords, color: 'var(--teal)' },
    { label: t.weekDistinctItems,value: report.distinct_items, sub: t.byItem,           color: 'var(--green)' },
    { label: t.weekLocations,    value: report.locations_count,sub: t.byLocation,       color: 'var(--amber)' },
  ] : [];

  async function saveStock(body) {
    setSaving(true);
    try {
      const res = await apiFetch(`/consumable-items/${adjusting.id}/stock`, {method:'PATCH', body, token});
      replaceItem(res.consumable_item);
      setAdjusting(null);
      setToast(t.toastStockUpdated);
      if (historyFor !== null) loadMovements(historyFor);
    } catch(ex) {
      setToast('Error: ' + ex.message);
    } finally {
      setSaving(false);
    }
  }

  async function saveReorderLevel(item, level) {
    try {
      const res = await apiFetch(`/consumable-items/${item.id}/reorder-level`, {method:'PATCH', body:{reorder_level: level}, token});
      replaceItem(res.consumable_item);
      setToast(t.toastReorderSaved);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  function loadMovements(itemID) {
    setHistoryLoading(true);
    apiFetch(`/consumable-stock-movements?item_id=${itemID}&limit=100`, {token})
      .then(res => setMovements(res.movements || []))
      .catch(() => setMovements([]))
      .finally(() => setHistoryLoading(false));
  }

  function toggleHistory(item) {
    if (historyFor === item.id) { setHistoryFor(null); setMovements([]); return; }
    setHistoryFor(item.id);
    loadMovements(item.id);
  }

  const lowCount = catalog.filter(ci => ci.low_stock).length;

  // Two kinds of bar on this page, so two passes: the weekly-report rows grow
  // sideways from the left, the day histogram grows upward from its baseline.
  //
  // `view` is in the dependency list and not just `report`, which is the whole
  // trick. These bars only exist while view==='report', and the report is
  // fetched on mount -- so keying on `report` alone runs the tween while the Log
  // tab is still on screen and the bars are not in the DOM, and then nothing
  // happens when the user actually switches over. Keying on the view as well
  // means the bars grow as they appear.
  const barsRef = useAnimate((g, root) => {
    MOTION.grow(g, root.querySelectorAll('.week-bar-fill'), 'x', 0.04);
    MOTION.grow(g, root.querySelectorAll('.daychart .fill'), 'y', 0.03, 7);
  }, [report, view]);

  return (
    <div ref={barsRef}>
      <div className="team-viewbar" style={{marginBottom:16}}>
        <div className="team-segmented">
          <button className={view==='log'?'active':''} onClick={()=>setView('log')}>
            {Ic.list}{t.consumablesLog}
          </button>
          <button className={view==='report'?'active':''} onClick={()=>setView('report')}>
            {Ic.report}{t.consumablesReport}
          </button>
        </div>
      </div>

      {view==='log' && (
        <>

      {/* ── Inventory: what is in stock right now ── */}
      <div className="table-card" style={{marginBottom:16}}>
        <div className="table-header">
          <span className="table-title">{t.inventory}</span>
          <div style={{display:'flex',alignItems:'center',gap:10}}>
            {lowCount > 0 && (
              <span className="badge" style={{background:'var(--amber-light)',color:'var(--amber)'}}>
                {lowCount} {t.lowStock}
              </span>
            )}
            <span style={{fontSize:12,color:'var(--gray-400)'}}>{catalog.length}</span>
          </div>
        </div>
        <div style={{padding:'13px 20px 2px',fontSize:12.5,color:'var(--gray-400)'}}>{t.inventorySubtitle}</div>
        {catalog.length === 0 ? (
          <div className="empty-state">{Ic.box}<p>{t.noConsumableItems}</p></div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>{t.consumableName}</th>
                <th>{t.inStock}</th>
                <th>{t.reorderLevel}</th>
                {canManage && <th></th>}
              </tr>
            </thead>
            <tbody>
              {catalog.map(ci=>(
                <React.Fragment key={ci.id}>
                  <tr>
                    <td>
                      <div style={{display:'flex',alignItems:'center',gap:10}}>
                        <ConsumableIcon name={ci.icon} size={30}/>
                        <span style={{fontWeight:500}}>{ci.name}</span>
                        {ci.low_stock && (
                          <span className="badge" style={{background:'var(--amber-light)',color:'var(--amber)',fontSize:10,padding:'1px 7px'}}>
                            {t.lowStock}
                          </span>
                        )}
                      </div>
                    </td>
                    <td>
                      <span style={{
                        fontFamily:'DM Mono,monospace', fontSize:14, fontWeight:600,
                        color: ci.stock < 0 ? 'var(--red)' : ci.low_stock ? 'var(--amber)' : 'var(--gray-900)',
                      }}>{ci.stock}</span>
                      {ci.stock < 0 && (
                        <span className="badge" style={{background:'var(--red-light)',color:'var(--red)',fontSize:10,padding:'1px 7px',marginLeft:7}}>
                          {t.stockOverdrawn}
                        </span>
                      )}
                    </td>
                    <td>
                      {canManage ? (
                        <input className="form-control" type="number" min="0" defaultValue={ci.reorder_level}
                          aria-label={`${ci.name} — ${t.reorderLevel}`}
                          style={{width:80,padding:'5px 8px'}}
                          // Saved when the field is left, and on Enter so the
                          // value is not silently lost by someone who types it
                          // and moves straight on.
                          onBlur={e=>{
                            const v = Number(e.target.value);
                            if (Number.isFinite(v) && v >= 0 && v !== ci.reorder_level) saveReorderLevel(ci, v);
                          }}
                          onKeyDown={e=>{ if (e.key === 'Enter') e.currentTarget.blur(); }}/>
                      ) : (
                        <span style={{fontSize:13,color:'var(--gray-400)'}}>{ci.reorder_level || '—'}</span>
                      )}
                    </td>
                    {canManage && (
                      <td style={{textAlign:'right'}}>
                        <div className="action-cell" style={{justifyContent:'flex-end'}}>
                          <button className="btn btn-ghost" style={{fontSize:12,padding:'4px 10px'}}
                            onClick={()=>setAdjusting(ci)}>
                            {Ic.edit} {t.adjustStock}
                          </button>
                          <button className="icon-btn" title={t.history} onClick={()=>toggleHistory(ci)}>
                            {Ic.clock}
                          </button>
                        </div>
                      </td>
                    )}
                  </tr>
                  {historyFor === ci.id && (
                    <tr>
                      <td colSpan={canManage ? 4 : 3} style={{background:'var(--gray-100)'}}>
                        {historyLoading ? (
                          <p style={{fontSize:12.5,color:'var(--gray-400)',margin:0}}>{t.analyticsLoading}</p>
                        ) : movements.length === 0 ? (
                          <p style={{fontSize:12.5,color:'var(--gray-400)',margin:0}}>{t.noStockMoves}</p>
                        ) : (
                          <table>
                            <thead>
                              <tr>
                                <th>{t.date}</th><th></th><th>{t.inStock}</th>
                                <th>{t.stockReason}</th><th>{t.loggedBy}</th><th>{t.stockNote}</th>
                              </tr>
                            </thead>
                            <tbody>
                              {movements.map(m=>(
                                <tr key={m.id}>
                                  <td style={{fontSize:12,color:'var(--gray-400)',whiteSpace:'nowrap'}}>
                                    {new Date(m.created_at).toLocaleString()}
                                  </td>
                                  <td style={{fontFamily:'DM Mono,monospace',fontSize:13,fontWeight:600,
                                              color: m.change > 0 ? 'var(--green)' : 'var(--red)'}}>
                                    {m.change > 0 ? `+${m.change}` : m.change}
                                  </td>
                                  <td style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{m.balance_after}</td>
                                  <td style={{fontSize:12.5}}>{t['stockReason_'+m.reason] || m.reason}</td>
                                  <td style={{fontSize:12.5,color:'var(--gray-600)'}}>{shortName(m.logged_by_name) || '—'}</td>
                                  <td style={{fontSize:12.5,color:'var(--gray-500)'}}>{m.note || '—'}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>

          <div className="table-card" style={{marginBottom:16,padding:'14px 20px',display:'flex',alignItems:'center',gap:12,flexWrap:'wrap'}}>
            <p style={{fontSize:13,color:'var(--gray-400)',margin:0,flex:1,minWidth:200}}>{t.consumablesSubtitle}</p>
            <input type="date" className="form-control" style={{width:'auto'}} value={date} max={todayStr}
              aria-label={t.historyPickDate}
              onChange={e=>setDate(e.target.value)} />
            <button className="btn btn-ghost" style={{fontSize:12,padding:'6px 12px'}} onClick={()=>setDate(todayStr)}>{t.historyToday}</button>
            <button className="btn btn-ghost" style={{fontSize:12,padding:'6px 12px'}} onClick={()=>setDate('')}>{t.historyAllDates}</button>
          </div>

          {summary.length > 0 && (
            <div className="analytics-grid">
              {summary.map(s=>(
                <div className="stat-card" key={s.item} style={{display:'flex',alignItems:'center',gap:12}}>
                  <ConsumableIcon name={s.icon} size={42}/>
                  <div style={{minWidth:0}}>
                    <div className="stat-label" style={{marginBottom:4}}>{s.item}</div>
                    <div className="stat-value">{s.quantity}</div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {usage.error ? (
            <div className="table-card">
              <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,margin:'14px 20px',display:'flex',alignItems:'center',gap:10}}>
                <span style={{flex:1}}>{usage.error}</span>
                <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
                  onClick={()=>setRetry(n => n + 1)}>{t.tryAgain}</button>
              </div>
            </div>
          ) : loading ? (
            <div className="empty-state"><p>{t.analyticsLoading}</p></div>
          ) : items.length === 0 ? (
            <div className="empty-state">{Ic.box}<p>{date ? t.consumablesEmpty : t.consumablesEmptyAll}</p></div>
          ) : (
            <div className="table-card">
              <table>
                <thead>
                  <tr><th>{t.date}</th><th>{t.item}</th><th>{t.quantity}</th><th>{t.location}</th><th>{t.loggedBy}</th></tr>
                </thead>
                <tbody>
                  {items.map(c=>(
                    <tr key={c.id}>
                      <td style={{fontSize:12,color:'var(--gray-400)',whiteSpace:'nowrap'}}>{new Date(c.created_at).toLocaleString()}</td>
                      <td>
                        <div style={{display:'flex',alignItems:'center',gap:10}}>
                          <ConsumableIcon name={c.icon} size={30}/>
                          <span style={{fontWeight:500}}>{c.item}</span>
                        </div>
                      </td>
                      <td style={{fontFamily:'DM Mono,monospace',fontSize:13}}>×{c.quantity}</td>
                      <td>{c.location}</td>
                      <td>{shortName(c.logged_by_name)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <LoadMore list={usage} t={t}/>
        </>
      )}

      {view==='report' && (
        <>
          <div className="table-card" style={{marginBottom:16,padding:'14px 20px',display:'flex',alignItems:'center',gap:12,flexWrap:'wrap'}}>
            <div className="team-segmented">
              <button className={!isMonthly?'active':''} onClick={()=>setPeriod('week')}>{t.weekly}</button>
              <button className={isMonthly?'active':''} onClick={()=>setPeriod('month')}>{t.monthly}</button>
            </div>
            <div className="week-nav">
              <button onClick={()=>shiftPeriod(-1)} title={isMonthly?t.prevMonth:t.prevWeek} aria-label={isMonthly?t.prevMonth:t.prevWeek}>
                <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M10 3L5 8l5 5"/></svg>
              </button>
              <span className="label">
                {report ? `${report.from_date} → ${report.to_date}` : isMonthly ? `${t.monthOf} ${month}` : `${t.weekOf} ${anchor}`}
              </span>
              <button onClick={()=>shiftPeriod(1)} title={isMonthly?t.nextMonth:t.nextWeek} aria-label={isMonthly?t.nextMonth:t.nextWeek}>
                <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M6 3l5 5-5 5"/></svg>
              </button>
            </div>
            <button className="btn btn-ghost" style={{fontSize:12,padding:'6px 12px'}} onClick={currentPeriod}>
              {isMonthly ? t.thisMonth : t.thisWeek}
            </button>
            {isMonthly && (
              <input type="month" className="form-control" style={{width:'auto'}} value={month}
                aria-label={t.monthOf}
                onChange={e=>{ if (e.target.value) setMonth(e.target.value); }} />
            )}
            <button className="btn btn-primary" style={{marginLeft:'auto'}}
              onClick={downloadReportPDF} disabled={!report || downloading}>
              {Ic.download} {downloading ? t.generatingPdf : t.downloadReportPdf}
            </button>
          </div>

          {rError && (
            <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,marginBottom:16}}>
              {rError}
            </div>
          )}

          {rLoading ? (
            <div className="empty-state"><p>{t.analyticsLoading}</p></div>
          ) : !report || report.total_records === 0 ? (
            <div className="empty-state">{Ic.box}<p>{isMonthly ? t.nothingUsedThisMonth : t.nothingUsedThisWeek}</p></div>
          ) : (
            <>
              <div className="analytics-grid" style={{marginBottom:20}}>
                {statCards.map(c=>(
                  <div className="stat-card" key={c.label}>
                    <div className="stat-label">{c.label}</div>
                    <div className="stat-value" style={{color:c.color}}>{c.value}</div>
                    <div className="stat-sub">{c.sub}</div>
                  </div>
                ))}
              </div>

              <div className="mini-card" style={{marginBottom:20}}>
                <div className="mini-card-title">{t.usageByDay}</div>
                <div className={`daychart${isMonthly?' compact':''}`}
                  style={{gridTemplateColumns:`repeat(${dayCells.length || 1}, minmax(0, 1fr))`}}>
                  {dayCells.map(d=>(
                    <div className={`daycol${d.qty===0?' empty':''}`} key={d.key}>
                      <div className="qty">{d.qty}</div>
                      <div className="track"><div className="fill" style={{height:`${d.height}%`}}/></div>
                      {!isMonthly && <div className="dow">{d.dow}</div>}
                      <div className="dom">{d.dom}</div>
                    </div>
                  ))}
                </div>
              </div>

              <div className="week-breakdowns" style={{marginBottom:20}}>
                <div className="mini-card">
                  <div className="mini-card-title">{t.byItem}</div>
                  {(report.by_item||[]).map(x=>(
                    <div className="week-bar-row" key={x.item}>
                      <div className="week-bar-label">
                        <ConsumableIcon name={x.icon} size={22}/>
                        <span>{x.item}</span>
                      </div>
                      <div className="week-bar-track">
                        <div className="week-bar-fill" style={{width:`${Math.max(4,(x.quantity/itemMax)*100)}%`,background:'var(--blue)'}}/>
                      </div>
                      <div className="week-bar-count">{x.quantity}</div>
                    </div>
                  ))}
                </div>

                <div className="mini-card">
                  <div className="mini-card-title">{t.byLocation}</div>
                  {locRows.slice(0, locLimit).map(x=>(
                    <div className="week-bar-row" key={`${x.mode}:${x.location}`}>
                      <div className="week-bar-label"><span>{x.location}</span></div>
                      <div className="week-bar-track">
                        <div className="week-bar-fill" style={{width:`${Math.max(4,(x.quantity/locMax)*100)}%`,background:'var(--teal)'}}/>
                      </div>
                      <div className="week-bar-count">{x.quantity}</div>
                    </div>
                  ))}
                  {locRows.length > locLimit && (
                    <div style={{fontSize:12,color:'var(--gray-400)',paddingTop:8}}>
                      {t.reportMore.replace('{n}', locRows.length - locLimit)}
                    </div>
                  )}
                </div>

                <div className="mini-card">
                  <div className="mini-card-title">{t.byTechnician}</div>
                  {(report.by_technician||[]).map(x=>(
                    <div className="week-bar-row" key={x.name}>
                      <div className="week-bar-label">
                        <Avatar name={x.name} idx={x.avatar_idx} size="sm"/>
                        <span>{x.name}</span>
                      </div>
                      <div className="week-bar-track">
                        <div className="week-bar-fill" style={{width:`${Math.max(4,(x.quantity/techMax)*100)}%`,background:'var(--green)'}}/>
                      </div>
                      <div className="week-bar-count">{x.quantity}</div>
                    </div>
                  ))}
                </div>
              </div>

              <EntryTable rows={aptRows}  locationHeader={t.apartmentUsage}/>
              <EntryTable rows={deptRows} locationHeader={t.departmentUsage}/>
            </>
          )}
        </>
      )}

      {adjusting && (
        <StockAdjustDialog item={adjusting} saving={saving} t={t}
          onCancel={()=>setAdjusting(null)} onSave={saveStock}/>
      )}
    </div>
  );
}


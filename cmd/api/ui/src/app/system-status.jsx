/* ════════════════ ADMIN PAGE (managers only) ════════════════ */
/* ════════════════ SYSTEM STATUS ════════════════ */
function sysTone(value, warn, bad) {          // lower is better (usage %, latency ms)
  if (value == null) return 'gray';
  if (value >= bad) return 'red';
  if (value >= warn) return 'amber';
  return 'green';
}

function cacheTone(percent, hasReads) {       // here higher is better
  if (!hasReads) return 'gray';
  if (percent >= 95) return 'green';
  if (percent >= 80) return 'amber';
  return 'red';
}

function formatBytes(bytes) {
  if (!bytes && bytes !== 0) return '—';
  const units = ['B','KB','MB','GB','TB'];
  let v = bytes, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

function formatUptime(totalSeconds, t) {
  const d = Math.floor(totalSeconds / 86400);
  const h = Math.floor((totalSeconds % 86400) / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

// One of the four headline cards: an icon, a big value, and a usage gauge underneath.
function SysCard({ icon, label, tone, value, unit, gauge, sub, na }) {
  // The gauge refills every 60s as fresh readings arrive, so it is keyed on the
  // reading rather than on mount: each new value is worth watching move.
  const root = useAnimate((g, el, opts) => {
    MOTION.grow(g, el.querySelectorAll('.sys-gauge-fill'), 'x', 0, 1);
  }, [gauge]);
  return (
    <div className="sys-card" ref={root}>
      <div className="sys-card-head">
        <span className="sys-card-label">{label}</span>
        <span className={`sys-icon tone-${tone}`}>{icon}</span>
      </div>
      <div className={`sys-value tone-${tone}`}>{value}{unit && <span className="sys-unit">{unit}</span>}</div>
      {gauge != null && (
        <div className="sys-gauge-track"><div className={`sys-gauge-fill tone-${tone}`} style={{width:`${Math.min(gauge,100)}%`}}/></div>
      )}
      {na ? <div className="sys-na">{na}</div> : sub && <div className="sys-sub">{sub}</div>}
    </div>
  );
}

// Server health: CPU, RAM, and round-trip latency to the server and the database.
// Refetches on a fixed 60s clock (see REFRESH_MS) for as long as this page stays open.
const SYS_REFRESH_MS = 60000;

function SystemStatusPage({ token }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [data, setData] = useState(null);
  const [serverLatency, setServerLatency] = useState(null);
  const [err, setErr] = useState('');
  const [loading, setLoading] = useState(true);
  const [secondsAgo, setSecondsAgo] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const lastFetchedAt = useRef(null);

  // manual: true for the on-demand button, so it can show its own spinner
  // without disturbing the (much less frequent) full-page loading state.
  const load = useCallback(async (manual) => {
    if (manual) setRefreshing(true);
    const start = performance.now();
    try {
      const res = await apiFetch('/system/status', { token });
      setServerLatency(Math.round(performance.now() - start));
      setData(res.system);
      setErr('');
    } catch (ex) {
      setErr(ex.message);
    } finally {
      lastFetchedAt.current = Date.now();
      setSecondsAgo(0);
      setLoading(false);
      if (manual) setRefreshing(false);
    }
  }, [token]);

  useEffect(() => {
    load(false);
    // The interval keeps its own fixed 60s cadence regardless of manual refreshes —
    // clicking "refresh now" gets you fresh data immediately without resetting the clock.
    const refresh = setInterval(load, SYS_REFRESH_MS);
    const tick = setInterval(() => {
      if (lastFetchedAt.current) setSecondsAgo(Math.round((Date.now() - lastFetchedAt.current) / 1000));
    }, 1000);
    return () => { clearInterval(refresh); clearInterval(tick); };
  }, [load]);

  if (loading) return <div className="empty-state">{Ic.server}<p>{t.sysRefreshing}</p></div>;

  const stale = !!err;
  const cpu = data?.cpu;
  const mem = data?.memory;
  const db = data?.database;
  const pgDbs = db?.databases || null;
  const pgStarted = db?.server_started_at ? new Date(db.server_started_at) : null;
  const pgTotalSize = pgDbs ? pgDbs.reduce((sum, d) => sum + (d.size_bytes || 0), 0) : 0;
  const cpuTone = cpu?.available ? sysTone(cpu.percent, 60, 85) : 'gray';
  const memTone = mem?.system_wide ? sysTone(mem.percent, 70, 90) : 'gray';
  const srvTone = sysTone(serverLatency, 100, 300);
  const dbTone = db ? (db.reachable ? sysTone(db.latency_ms, 100, 300) : 'red') : 'gray';

  return (
    <div>
      <div style={{display:'flex', alignItems:'center', justifyContent:'space-between', marginBottom:18, flexWrap:'wrap', gap:10}}>
        <p style={{fontSize:13, color:'var(--gray-400)', margin:0}}>{t.systemStatusSub}</p>
        <div className="sys-live">
          <span className={`sys-live-dot ${stale ? 'stale' : ''}`}/>
          {stale ? t.sysStale : (secondsAgo < 2 ? t.sysUpdatedJustNow : t.sysUpdatedAgo.replace('{n}', secondsAgo))}
          <span style={{opacity:0.6}}>· {t.sysAutoRefresh}</span>
          <button type="button" className={`sys-refresh-btn ${refreshing ? 'spinning' : ''}`}
            onClick={()=>load(true)} disabled={refreshing} title={t.sysRefreshNow} aria-label={t.sysRefreshNow}>
            {Ic.refresh}
          </button>
        </div>
      </div>

      <div className="sys-grid">
        <SysCard icon={Ic.cpu} label={t.cpuUsage} tone={cpuTone}
          value={cpu?.available ? cpu.percent : '—'} unit={cpu?.available ? '%' : ''}
          gauge={cpu?.available ? cpu.percent : null}
          sub={cpu?.available ? `${cpu.cores} ${t.sysCores}` : null}
          na={!cpu?.available ? t.sysCpuUnavailable.replace('{os}', data?.os || '') : null}/>

        <SysCard icon={Ic.server} label={t.ramUsage} tone={memTone}
          value={mem?.system_wide ? mem.percent : formatBytes(mem?.used_bytes)}
          unit={mem?.system_wide ? '%' : ''}
          gauge={mem?.system_wide ? mem.percent : null}
          sub={mem?.system_wide ? `${formatBytes(mem.used_bytes)} / ${formatBytes(mem.total_bytes)}` : null}
          na={!mem?.system_wide ? t.sysProcessOnly.replace('{os}', data?.os || '') : null}/>

        <SysCard icon={Ic.activity} label={t.latencyServer} tone={srvTone}
          value={serverLatency ?? '—'} unit="ms"
          sub="Round trip from your browser"/>

        <SysCard icon={Ic.database} label={t.latencyDatabase} tone={dbTone}
          value={db?.reachable ? db.latency_ms : (db ? '—' : '—')} unit={db?.reachable ? 'ms' : ''}
          sub={db && !db.reachable ? null : (db ? t.dbConnectionsSub.replace('{inUse}',db.in_use).replace('{idle}',db.idle).replace('{max}',db.max_open_connections) : null)}
          na={db && !db.reachable ? t.dbUnreachable : null}/>
      </div>

      <div className="mini-card">
        <div className="mini-card-title">{t.systemStatus}</div>
        <div className="sys-detail-grid">
          <div className="sys-detail-item"><div className="sys-detail-label">{t.uptime}</div><div className="sys-detail-value">{data ? formatUptime(data.uptime_seconds, t) : '—'}</div></div>
          <div className="sys-detail-item"><div className="sys-detail-label">{t.goroutines}</div><div className="sys-detail-value">{data?.goroutines ?? '—'}</div></div>
          <div className="sys-detail-item"><div className="sys-detail-label">{t.serverOs}</div><div className="sys-detail-value" style={{textTransform:'capitalize'}}>{data?.os || '—'}</div></div>
          <div className="sys-detail-item"><div className="sys-detail-label">{t.goVersion}</div><div className="sys-detail-value">{data?.go_version || '—'}</div></div>
        </div>
      </div>

      {pgDbs && (
        <>
          <div className="mini-card" style={{marginTop:16,marginBottom:16}}>
            <div className="mini-card-title">{t.pgInstance}</div>
            <div className="sys-detail-grid">
              <div className="sys-detail-item"><div className="sys-detail-label">{t.pgVersion}</div><div className="sys-detail-value">{db.server_version || '—'}</div></div>
              <div className="sys-detail-item"
                title={pgStarted ? pgStarted.toLocaleString() : undefined}>
                <div className="sys-detail-label">{t.pgUptime}</div>
                <div className="sys-detail-value">
                  {pgStarted ? formatUptime(Math.max(0, (Date.now() - pgStarted.getTime()) / 1000), t) : '—'}
                </div>
              </div>
              <div className="sys-detail-item"><div className="sys-detail-label">{t.pgDatabaseCount}</div><div className="sys-detail-value">{pgDbs.length}</div></div>
              <div className="sys-detail-item"><div className="sys-detail-label">{t.pgTotalSize}</div><div className="sys-detail-value">{formatBytes(pgTotalSize)}</div></div>
            </div>
          </div>

          <div className="table-card">
            <div className="table-header">
              <span className="table-title">{t.pgDatabases}</span>
              <span style={{fontSize:12,color:'var(--gray-400)'}}>{pgDbs.length}</span>
            </div>
            <TableScroll label={t.pgDatabases}>
              <table>
                <thead>
                  <tr>
                    <th>{t.pgDatabase}</th>
                    <th>{t.pgSize}</th>
                    <th>{t.pgConnections}</th>
                    <th>{t.pgTransactions}</th>
                    <th>{t.pgCacheHit}</th>
                    <th>{t.pgDeadlocks}</th>
                    <th>{t.pgTempFiles}</th>
                    <th>{t.pgStatsSince}</th>
                  </tr>
                </thead>
                <tbody>
                  {pgDbs.map(d=>{
                    const hasReads = (d.blocks_hit || 0) + (d.blocks_read || 0) > 0;
                    const tone = cacheTone(d.cache_hit_percent, hasReads);
                    return (
                      <tr key={d.name}>
                        <td>
                          <div style={{display:'flex',alignItems:'center',gap:8,flexWrap:'wrap'}}>
                            <span style={{fontWeight:500}}>{d.name}</span>
                            {d.current && (
                              <span className="badge" style={{background:'var(--blue-light)',color:'var(--blue)'}}>{t.pgCurrent}</span>
                            )}
                            {!d.allow_connections && (
                              <span className="badge" style={{background:'var(--gray-100)',color:'var(--gray-500)'}}>{t.pgNoConnect}</span>
                            )}
                          </div>
                        </td>
                        <td style={{fontFamily:'DM Mono,monospace',fontSize:12.5,whiteSpace:'nowrap'}}>{formatBytes(d.size_bytes)}</td>
                        <td style={{fontVariantNumeric:'tabular-nums'}}>{d.active_connections}</td>
                        <td style={{fontVariantNumeric:'tabular-nums'}}>{(d.transactions || 0).toLocaleString()}</td>
                        <td>
                          {hasReads ? (
                            <div style={{display:'flex',alignItems:'center',gap:8,minWidth:110}}
                              title={`${(d.blocks_hit||0).toLocaleString()} / ${((d.blocks_hit||0)+(d.blocks_read||0)).toLocaleString()}`}>
                              <div className="sys-gauge-track" style={{flex:1,marginTop:0}}>
                                <div className={`sys-gauge-fill tone-${tone}`} style={{width:`${d.cache_hit_percent}%`}}/>
                              </div>
                              <span style={{fontSize:12,color:'var(--gray-600)',fontVariantNumeric:'tabular-nums'}}>{d.cache_hit_percent.toFixed(1)}%</span>
                            </div>
                          ) : <span style={{color:'var(--gray-400)'}}>—</span>}
                        </td>
                        <td style={{fontVariantNumeric:'tabular-nums'}}>{d.deadlocks}</td>
                        <td style={{fontVariantNumeric:'tabular-nums'}}>{(d.temp_files || 0).toLocaleString()}</td>
                        <td style={{fontSize:12,color:'var(--gray-400)',whiteSpace:'nowrap'}}>
                          {d.stats_reset ? new Date(d.stats_reset).toLocaleString() : t.pgSinceStart}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </TableScroll>
          </div>
        </>
      )}

      {db?.stats_error && (
        <div className="table-card" style={{marginTop:16,padding:'16px 20px'}}>
          <div style={{fontSize:13,fontWeight:600,color:'var(--amber)'}}>{t.pgStatsUnavailable}</div>
          <div style={{fontSize:12.5,color:'var(--gray-400)',marginTop:4}}>{t.pgStatsUnavailableHint}</div>
          <div style={{fontSize:12,color:'var(--gray-400)',marginTop:8,fontFamily:'DM Mono,monospace'}}>{db.stats_error}</div>
        </div>
      )}
    </div>
  );
}

/* ── collapsed catalog lists ──
   Issue types, departments, Connecta agents and consumables are reference lists:
   most visits are about the one at the top, and the tail is scrolled past. So each
   shows its first few rows and opens on request.

   One constant and one button for all four, deliberately. Four copies of the same
   threshold and the same markup is how three of them end up at five and one at
   six, and a page where one list in a column behaves differently from the one above
   it reads as a bug rather than as a choice.

   Five rows, not five <tr>s. A consumable whose icon is being edited renders a
   second row underneath it, and counting rows would make that item look like two
   and hide a sixth item's worth of nothing.

   The count in each card's header is the real total and is left alone, so the
   length of the list is never a surprise -- only its tail is deferred. */

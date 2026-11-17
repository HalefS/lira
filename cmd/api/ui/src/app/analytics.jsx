function analyticsChange(current, previous, goodDirection) {
  if (previous === 0) {
    if (current === 0) return null;
    const dir = current > 0 ? 'up' : 'down';
    return { pct: null, dir, color: analyticsChangeColor(dir, goodDirection) };
  }
  const pct = ((current - previous) / previous) * 100;
  if (Math.abs(pct) < 0.5) return { pct: 0, dir: 'flat', color: 'var(--gray-400)' };
  const dir = pct > 0 ? 'up' : 'down';
  return { pct, dir, color: analyticsChangeColor(dir, goodDirection) };
}
function analyticsChangeColor(dir, goodDirection) {
  if (goodDirection === 'neutral') return 'var(--gray-400)';
  const isGood = (dir === 'up' && goodDirection === 'up') || (dir === 'down' && goodDirection === 'down');
  return isGood ? 'var(--green)' : 'var(--red)';
}

function AnalyticsMetricCard({ label, current, previous, suffix, decimals, goodDirection, t }) {
  const change = analyticsChange(current, previous, goodDirection);
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{current.toFixed(decimals||0)}{suffix||''}</div>
      <div className="stat-sub">
        {change && change.dir !== 'flat' && (
          <span style={{color:change.color,fontWeight:600}}>
            {change.dir==='up' ? '▲' : '▼'} {change.pct!=null ? Math.abs(change.pct).toFixed(0)+'%' : ''}
          </span>
        )}
        <span style={{color:'var(--gray-400)'}}>{t.vsPrevious}</span>
      </div>
    </div>
  );
}

function AnalyticsPage({ token, alertsPendingCount }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [range, setRange] = useState('week');
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    apiFetch(`/analytics?range=${range}`, {token})
      .then(res => { if (!cancelled) setData(res.analytics); })
      .catch(() => {})
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [range, token]);

  const bestTech = data && data.best_technician_id != null
    ? data.by_technician.find(tech => tech.user_id === data.best_technician_id)
    : null;

  const techByAvg = data ? [...data.by_technician].filter(x=>x.resolved>0).sort((a,b)=>a.avg_time_minutes-b.avg_time_minutes) : [];

  const watchItems = [];
  if (data) {
    const pendingChange = analyticsChange(data.pending.current, data.pending.previous, 'down');
    if (pendingChange && pendingChange.dir==='up') {
      watchItems.push(lang==='pt'
        ? `A pendência subiu de ${data.pending.previous} para ${data.pending.current} avarias.`
        : `Pending backlog rose from ${data.pending.previous} to ${data.pending.current} issues.`);
    }
    const avgChange = analyticsChange(data.avg_time_minutes.current, data.avg_time_minutes.previous, 'down');
    if (avgChange && avgChange.dir==='up') {
      watchItems.push(lang==='pt'
        ? `O tempo médio de resolução aumentou ${Math.abs(avgChange.pct).toFixed(0)}%.`
        : `Average resolution time increased ${Math.abs(avgChange.pct).toFixed(0)}%.`);
    }
    const rateChange = analyticsChange(data.resolution_rate.current, data.resolution_rate.previous, 'up');
    if (rateChange && rateChange.dir==='down') {
      watchItems.push(lang==='pt'
        ? `A taxa de resolução desceu de ${data.resolution_rate.previous}% para ${data.resolution_rate.current}%.`
        : `Resolution rate dropped from ${data.resolution_rate.previous}% to ${data.resolution_rate.current}%.`);
    }
    if (alertsPendingCount > 0) {
      watchItems.push(lang==='pt'
        ? `${alertsPendingCount} avaria(s) recorrente(s) ainda por resolver.`
        : `${alertsPendingCount} recurring issue${alertsPendingCount===1?'':'s'} still unresolved.`);
    }
  }

  return (
    <div>
      <div className="table-card" style={{marginBottom:16,padding:'14px 20px',display:'flex',justifyContent:'space-between',alignItems:'center',flexWrap:'wrap',gap:10}}>
        <p style={{fontSize:13,color:'var(--gray-400)',margin:0}}>{t.analyticsSubtitle}</p>
        <div style={{display:'flex',gap:8}}>
          <button className={`chip ${range==='week'?'active':''}`} onClick={()=>setRange('week')}>{t.rangeWeek}</button>
          <button className={`chip ${range==='month'?'active':''}`} onClick={()=>setRange('month')}>{t.rangeMonth}</button>
        </div>
      </div>

      {loading ? (
        <div className="empty-state"><p>{t.analyticsLoading}</p></div>
      ) : !data || data.total_issues.current + data.total_issues.previous === 0 ? (
        <div className="empty-state">{Ic.trending}<p>{t.analyticsNoData}</p></div>
      ) : (
        <>
          <div className="analytics-grid">
            <AnalyticsMetricCard t={t} label={t.totalIssuesMetric} current={data.total_issues.current} previous={data.total_issues.previous} goodDirection="neutral"/>
            <AnalyticsMetricCard t={t} label={t.resolvedMetric} current={data.resolved.current} previous={data.resolved.previous} goodDirection="up"/>
            <AnalyticsMetricCard t={t} label={t.pendingMetric} current={data.pending.current} previous={data.pending.previous} goodDirection="down"/>
            <AnalyticsMetricCard t={t} label={t.resolutionRateMetric} current={data.resolution_rate.current} previous={data.resolution_rate.previous} suffix="%" decimals={0} goodDirection="up"/>
            <AnalyticsMetricCard t={t} label={t.avgTimeMetric} current={data.avg_time_minutes.current} previous={data.avg_time_minutes.previous} suffix="m" decimals={1} goodDirection="down"/>
            <AnalyticsMetricCard t={t} label={t.fastestTimeMetric} current={data.fastest_time_minutes.current} previous={data.fastest_time_minutes.previous} suffix="m" decimals={0} goodDirection="down"/>
            <AnalyticsMetricCard t={t} label={t.recurringAlertsMetric} current={data.recurring_alerts_opened.current} previous={data.recurring_alerts_opened.previous} goodDirection="down"/>
            <AnalyticsMetricCard t={t} label={t.falsePositivesMetric} current={data.false_positives.current} previous={data.false_positives.previous} goodDirection="down"/>
            <AnalyticsMetricCard t={t} label={t.falsePositiveRateMetric} current={data.false_positive_rate.current} previous={data.false_positive_rate.previous} suffix="%" decimals={1} goodDirection="down"/>
          </div>

          {bestTech && (
            <div className="mini-card" style={{marginBottom:20,display:'flex',alignItems:'center',gap:14}}>
              <span style={{color:'var(--green)'}}>{Ic.check}</span>
              <div>
                <div className="mini-card-title" style={{marginBottom:2}}>{t.bestAvgTime}: {shortName(bestTech.name)}</div>
                <div style={{fontSize:13,color:'var(--gray-400)'}}>{bestTech.avg_time_minutes}m {t.avgTimeMetric.toLowerCase()} · {bestTech.resolved} {t.resolvedMetric.toLowerCase()}</div>
              </div>
            </div>
          )}

          {watchItems.length > 0 ? (
            <div className="mini-card" style={{marginBottom:20,background:'var(--amber-light)',border:'none'}}>
              <div className="mini-card-title" style={{marginBottom:8,color:'var(--amber)'}}>{t.thingsToWatch}</div>
              <ul style={{margin:0,paddingLeft:18,fontSize:13,color:'var(--gray-900)'}}>
                {watchItems.map((w,i)=><li key={i} style={{marginBottom:4}}>{w}</li>)}
              </ul>
            </div>
          ) : (
            <div className="mini-card" style={{marginBottom:20,background:'var(--green-light)',border:'none'}}>
              <div style={{fontSize:13,color:'var(--green)',fontWeight:500}}>✓ {t.allGood}</div>
            </div>
          )}

          <div className="admin-grid">
            <div className="table-card">
              <div className="table-header"><span className="table-title">{t.teamPerformance}</span></div>
              <table>
                <thead><tr><th>{t.loggedBy}</th><th>{t.issuesLogged}</th><th>{t.resolvedMetric}</th><th>{t.avgTimeMetric}</th></tr></thead>
                <tbody>
                  {techByAvg.map(tech=>(
                    <tr key={tech.user_id}>
                      <td>
                        {shortName(tech.name)}
                        {data.best_technician_id===tech.user_id && (
                          <span className="badge badge-ok" style={{marginLeft:8,fontSize:10}}>{t.bestLabel}</span>
                        )}
                      </td>
                      <td>{tech.issues_logged}</td>
                      <td>{tech.resolved}</td>
                      <td>{tech.avg_time_minutes}m</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="table-card">
              <div className="table-header"><span className="table-title">{t.typeBreakdown}</span></div>
              <table>
                <thead><tr><th>{t.type}</th><th>{t.thisPeriod}</th><th>{t.previousPeriod}</th><th>{t.falsePositivesMetric}</th></tr></thead>
                <tbody>
                  {data.by_type.map(bt=>(
                    <tr key={bt.type}>
                      <td><TypeBadge type={bt.type}/></td>
                      <td>{bt.current}</td>
                      <td style={{color:'var(--gray-400)'}}>{bt.previous}</td>
                      <td style={{color: bt.false_positives ? 'var(--amber)' : 'var(--gray-400)', fontWeight: bt.false_positives ? 600 : 400}}>{bt.false_positives || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  );
}


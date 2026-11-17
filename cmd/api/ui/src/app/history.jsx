/* ════════════════ HISTORY PAGE ════════════════ */
function HistoryPage({ token, issueTypes, epoch=0, currentUser, slaMinutes, onDelete, onEdit }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const todayStr = localISODate(new Date());
  const [date, setDate] = useState('');
  /* Both tables below ask the same endpoint, so when /issues is unreachable they
     both fail at once and each printed its own red banner with the same text.
     Which of them failed is tracked here instead, and only one banner is drawn:
     the apt table's, because it is the one that comes first on the page. The
     retry counter is shared for the same reason -- one Try again re-asks both
     questions, rather than fixing whichever table the button happened to be in
     and leaving the other showing the stale failure. */
  const [failures, setFailures] = useState({});
  const [retrySignal, setRetrySignal] = useState(0);
  const noteFailure = useCallback((mode, error) => {
    setFailures(prev => prev[mode] === error ? prev : {...prev, [mode]: error});
  }, []);

  return (
    <div>
      <div className="table-card" style={{marginBottom:16,padding:'14px 20px',display:'flex',alignItems:'center',gap:12,flexWrap:'wrap'}}>
        <label htmlFor="history-date" style={{fontSize:13,fontWeight:600,color:'var(--gray-600)'}}>{t.historyPickDate}</label>
        <input
          id="history-date" type="date" className="form-control" style={{width:'auto'}}
          value={date} max={todayStr}
          onChange={e=>setDate(e.target.value)}
        />
        <button className="btn btn-ghost" style={{fontSize:12,padding:'8px 14px',minHeight:36}} onClick={()=>setDate(todayStr)}>{t.historyToday}</button>
        <button className="btn btn-ghost" style={{fontSize:12,padding:'8px 14px',minHeight:36}} onClick={()=>setDate('')}>{t.historyAllDates}</button>
        {/* Today's list is not just today's issues: anything still pending from an
            earlier day stays on it until it is solved, so the number of rows can
            be larger than the number logged today. Saying so is the difference
            between that looking like a bug and looking like the point. A past
            date is that date alone, which is why only today is mentioned. */}
        {!date
          ? <span style={{fontSize:12,color:'var(--gray-400)',marginLeft:'auto'}}>{t.historyShowingAll}</span>
          : date === todayStr &&
            <span style={{fontSize:12,color:'var(--gray-400)',marginLeft:'auto'}}>{t.historyTodayCarriesPending}</span>}
      </div>

      {/* Both tables are given the chosen date and ask the server for that day's
          rows. They used to receive the whole issue list and narrow it here,
          which is why this page needed every issue in memory to show one day of
          them. Each table now paginates on its own, so the two can be showing
          different amounts. */}
      <IssuesTable token={token} issueTypes={issueTypes} tableMode="apt"  date={date} epoch={epoch} currentUser={currentUser} slaMinutes={slaMinutes} onDelete={onDelete} onEdit={onEdit}
        onFailure={noteFailure} peerError={failures.dept} isPrimary retrySignal={retrySignal} onRetryAll={()=>setRetrySignal(n => n + 1)}
        emptyText={date ? t.historyNoIssues : t.noMatch}/>
      <IssuesTable token={token} issueTypes={issueTypes} tableMode="dept" date={date} epoch={epoch} currentUser={currentUser} slaMinutes={slaMinutes} onDelete={onDelete} onEdit={onEdit}
        onFailure={noteFailure} peerError={failures.apt} retrySignal={retrySignal} onRetryAll={()=>setRetrySignal(n => n + 1)}
        emptyText={date ? t.historyNoIssues : t.noMatch}/>
    </div>
  );
}


/* ════════════════ DASHBOARD ════════════════ */
/* One person in the "team today" band.
   The shift's own text times are printed rather than the instants from the payload:
   `shift.end_time` is already "16:00" as the hotel writes it, so formatting it is a
   no-op, while re-deriving an hour from a timestamp is exactly the mistake the server
   side spent this change fixing. */
function TodayPerson({ t, m, changed }) {
  const tone = m.status === 'working_now' ? 'is-now'
    : m.status === 'coming_soon' ? 'is-soon'
    : m.status === 'finished'   ? 'is-done'
    : 'is-away';
  const mark = m.status === 'working_now' ? Ic.check
    : m.status === 'coming_soon' ? Ic.clock
    : m.status === 'away'       ? Ic.slash
    : Ic.check;

  // Only working_now and coming_soon have a useful time, and they want DIFFERENT ones:
  // "until 16:00" is how long you have them, "from 19:00" is when they arrive. For
  // somebody who left at 09:00, "until 16:00" would be noise.
  const when = (() => {
    if (!m.shift) return '';
    if (m.status === 'working_now') {
      return t.todayUntil.replace('{time}', m.shift.end_time)
        + (m.from_yesterday ? '' : '');
    }
    if (m.status === 'coming_soon') return t.todayFrom.replace('{time}', m.shift.start_time);
    return m.shift.start_time + ' – ' + m.shift.end_time;
  })();

  return (
    <div className={`rota-person ${tone}${m.active ? '' : ' is-inactive'}${changed ? ' now-person-changed' : ''}`}>
      <Avatar name={shortName(m.name)} idx={m.avatar_idx}/>
      <span className="rota-person-id">
        <span className="rota-person-name">
          {shortName(m.name)}
          {/* The overnight tail. Without this, a member on until 06:00 reads as 06:00
              TONIGHT. Reuses the rota page's own mark and key verbatim. */}
          {m.from_yesterday && (
            <span className="rota-person-plus1"
              title={t.todayOvernightTitle.replace('{name}', shortName(m.name))}>
              {t.rotaPlusOne}
            </span>
          )}
        </span>
        <span className="rota-person-when">
          {m.shift ? <>{m.shift.name} · <span className="rota-person-time">{when}</span></> : ''}
        </span>
        {/* "+1" reads aloud as "plus one", which means nothing, so the same sentence
            that is in the tooltip is also in the accessible name. The cell is not a
            control, so aria-label cannot carry it. */}
        <span className="sr-only">
          {m.from_yesterday ? t.todayOvernightTitle.replace('{name}', shortName(m.name)) : ''}
        </span>
      </span>
      <span className="rota-person-mark" aria-hidden="true">{mark}</span>
    </div>
  );
}

/* "Who is on now", and the staleness rules that make it honest.
   A dashboard left open from 07:00 must not show a frozen 07:00. */
function useTodayRota() {
  const [data, setData] = useState(null);
  const [stale, setStale] = useState(false);
  const [shown, setShown] = useState({});   // status keyed by user_id, for the pulse
  const dayRef = useRef('');

  const load = useCallback(() => {
    // No token: /v1/today is PUBLIC, like /v1/schedule, because it is a strict subset of
    // what that already publishes.
    return apiFetch('/today')
      .then(res => {
        const d = res.today;
        if (!d) return;
        // The DAY ROLLED OVER while the tab sat open. The statuses were computed for
        // yesterday, so they are all wrong -- a stale flag alone would be a lie, since
        // the numbers look current.
        const rolled = dayRef.current !== '' && dayRef.current !== d.date;
        dayRef.current = d.date;
        setShown(prev => {
          const next = {};
          for (const m of d.members || []) next[m.user_id] = m.status;
          if (rolled) return {};
          for (const id in prev) if (!(id in next) || prev[id] !== next[id]) next['~' + id] = true;
          return next;
        });
        setData(d);
        setStale(false);
      })
      // Keep the LAST GOOD members and mark the dot stale. A dashboard that empties
      // itself on a network blip is strictly worse than one showing a slightly old
      // answer -- usePagedList documents the same rows-vs-error rule.
      .catch(() => setStale(true));
  }, []);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    const id = setInterval(load, 60000);
    // visibilitychange is the one that MATTERS: setInterval is throttled hard in a
    // background tab, so the interval alone is not trustworthy.
    const onVis = () => { if (!document.hidden) load(); };
    document.addEventListener('visibilitychange', onVis);
    return () => { clearInterval(id); document.removeEventListener('visibilitychange', onVis); };
  }, [load]);

  return { data, stale, load, changed: shown };
}

function TodayBand({ t, lang, rota, onGoToPage }) {
  const { data, stale, changed } = rota;
  const members = (data && data.members) || [];

  const groups = [
    { key: 'working_now',  label: t.todayWorking, cls: 'is-now',  cap: 10 },
    { key: 'coming_soon',  label: t.todayComing,  cls: 'is-soon', cap: 10 },
    { key: 'finished',     label: t.todayFinished, cls: 'is-done', cap: 4 },
    { key: 'away',         label: t.todayAway,    cls: 'is-away', cap: 4 },
  ];

  const asOf = data && data.now
    ? new Date(data.now).toLocaleTimeString(lang === 'pt' ? 'pt-PT' : 'en-GB',
        { hour: '2-digit', minute: '2-digit' })
    : '';

  return (
    <div className="mini-card now-band">
      <div className="now-band-head">
        <span className="mini-card-title">{t.todayTeam}</span>
        {data && (
          <span className="now-band-head-right">
            {/* Reuses .sys-live and its dot VERBATIM -- same pulse, same .stale grey --
                because that is already the app's liveness mark. The TEXT differs: System
                says "updated Ns ago", a live countdown. This says "as of 14:22", once,
                from the SERVER's instant, because the question is WHEN THIS WAS TRUE and
                not how old this is. */}
            <span className="now-asof">
              <span className={'sys-live-dot' + (stale ? ' stale' : '')}/>
              {stale ? t.todayStale : t.todayAsOf.replace('{time}', asOf)}
            </span>
            <button type="button" className="btn btn-ghost"
              onClick={() => onGoToPage('schedule')}>
              {t.rotaTitle} {Ic.chevron}
            </button>
          </span>
        )}
      </div>

      {!data && <p className="now-group-empty">{t.analyticsLoading}</p>}

      {data && members.length === 0 && (
        <p className="now-group-empty">{t.todayNoneWorking}</p>
      )}

      {groups.map(g => {
        const rows = members.filter(m => m.status === g.key);
        if (rows.length === 0) return null;
        const shownRows = rows.slice(0, g.cap);
        return (
          <div className="now-group" key={g.key}>
            <div className="now-group-head">
              {g.label}
              <span className="now-group-count">{rows.length}</span>
            </div>
            <div className="now-people">
              {shownRows.map(m => (
                <TodayPerson key={m.user_id} t={t} m={m}
                  changed={!!changed['~' + m.user_id]}/>
              ))}
            </div>
          </div>
        );
      })}

      {/* One footnote for the whole block, and only while somebody is affected. A +1 with
          no legend is a mark that means nothing. */}
      {data && members.some(m => m.from_yesterday) && (
        <p className="now-note">{t.todayOvernightNote}</p>
      )}
    </div>
  );
}

/* "Needs attention".
   NO new request: App already holds the whole consumables catalog from
   /consumable-items at boot, and each item carries a server-derived low_stock, so there
   is exactly ONE low-stock predicate in the app and this block reuses it rather than
   inventing a second that could disagree with the Consumables page.

   The filter is BROADER than low_stock on purpose: `stock <= 0` also catches a genuinely
   empty item whose reorder level was never set. That does not contradict the Consumables
   page, which answers a different question -- "which items are below a configured
   threshold?" -- and keeps showing "Low stock: 0" for the rest. */
function AttentionBand({ t, items, onGoToPage }) {
  const catalog = items || [];
  // Deliberately a DIFFERENT order from the Consumables page's, which sorts by name
  // because it is a catalog you look things up in. This is a triage list, so the most
  // depleted item has to be first. The id tail makes it total.
  const rows = catalog
    .filter(ci => (ci.stock ?? 0) <= 0 || ci.low_stock)
    .sort((a, b) => (a.stock ?? 0) - (b.stock ?? 0)
      || String(a.name).localeCompare(String(b.name))
      || a.id - b.id);

  const CAP = 8;
  const shown = rows.slice(0, CAP);
  const rest = rows.length - shown.length;

  return (
    <div className="mini-card now-band">
      <div className="now-band-head">
        <span className="mini-card-title">{t.attentionTitle}</span>
        <span className="now-band-head-right">
          <button type="button" className="btn btn-ghost" onClick={() => onGoToPage('consumables')}>
            {t.attentionOpen}
          </button>
        </span>
      </div>

      {/* The block ALWAYS renders, empty or not. A block that vanishes teaches the
          reader it does not exist, and absence-of-block on a dashboard is
          indistinguishable from absence-of-feature -- which is the whole answer to "how
          does anyone know it is here". The all-clear is deliberately QUIET ink and a
          small check rather than a green wash: this is the absence of a conclusion, not
          a conclusion, and a green card would be announcing good news. */}
      {catalog.length === 0 && <p className="now-group-empty">{t.noConsumableItems}</p>}

      {catalog.length > 0 && rows.length === 0 && (
        <span className="attn-allgood">{Ic.check} {t.attentionAllGood}</span>
      )}

      {shown.length > 0 && (
        <div className="attn-chips">
          {shown.map(ci => (
            <span className={'attn-chip' + ((ci.stock ?? 0) < 0 ? ' is-over' : '')}
              key={ci.id}
              title={(ci.low_stock || (ci.stock ?? 0) < 0)
                ? t.attentionAtLevel.replace('{n}', ci.reorder_level)
                : t.attentionEmpty}>
              <ConsumableIcon name={ci.icon} size={22}/>
              <span className="attn-name">{ci.name}</span>
              {/* The badge says WHY, and only when it adds something the number does
                  not. A chip reading "empty  0" says the same thing twice, which is
                  what the first screenshot showed for every item; the zero alone is
                  the alarm, and "empty" moves to the title where it explains the chip
                  to a pointer or a screen reader without repeating it visually. */}
              {(ci.stock ?? 0) < 0 ? <span className="badge badge-overdue">{t.stockOverdrawn}</span>
                : ci.low_stock ? <span className="badge badge-soon">{t.lowStock}</span>
                : null}
              <span className="attn-stock">{ci.stock ?? 0}</span>
            </span>
          ))}
          {/* A plain count, not a button: the block already has one link to the same
              destination, and the sort puts the LEAST urgent items in this tail. */}
          {rest > 0 && <span className="attn-more">{t.attentionAndMore.replace('{count}', rest)}</span>}
        </div>
      )}
    </div>
  );
}

function Dashboard({ users, stats, consumableItems, onGoToPage, digest, notifUnread, onRetryDigest }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];

  /* Everything here comes from /stats. It used to fall back to counting the issue
     list App had in memory while that request was in flight, which meant App had
     to hold every issue ever logged for the sake of a loading state that lasted
     a moment. /stats is fetched at start-up alongside everything else, so the
     numbers are simply absent until it lands. */
  const totalIssues  = stats ? stats.total_issues  : 0;
  const resolved     = stats ? stats.resolved       : 0;
  const byType       = stats ? stats.by_type        : {};
  const byTech       = stats ? stats.by_technician  : [];
  const maxWork      = Math.max(...(byTech.map(t=>t.count)), 1);
  const maxType      = Math.max(...Object.values(byType), 1);

  // Split out from avgTime so the counter has a number to tween. null is the
  // one case that is not a figure at all — nothing to average yet — and it has
  // to stay an em dash rather than count up from zero to nothing.
  const avgNum = stats ? Number(stats.avg_minutes) : null;
  const avgText = avgNum === null ? '—' : Number(avgNum).toFixed(1);

  // ONE rota fetch for the whole page, lifted out of TodayBand so the tile and the band
  // cannot disagree. Two requests would be two answers to "who is on", sixty seconds
  // apart, and the number in the tile would be the one nobody reads.
  const rota = useTodayRota();
  const rotaMembers = (rota.data && rota.data.members) || [];
  // null, not 0: while the answer is unknown the tile must show an em dash. "0 people
  // working" is a meaningful and WRONG answer, and it is the one a naive
  // `(rotaMembers || []).length` produces.
  const onNow = rota.data ? rotaMembers.filter(m => m.status === 'working_now').length : null;
  // Whoever is next, which is what the old tile should have been saying. Coming_soon is
  // already sorted by shift start, so the first of them is the soonest.
  const onNextCell = rotaMembers.find(m => m.status === 'coming_soon' && m.shift);
  const onNext = onNextCell ? { name: onNextCell.name, time: onNextCell.shift.start_time } : null;

  // The bars grow once the data behind them is settled. Keyed on the props rather
  // than on byTech/byType, because both are rebuilt into a fresh object on every
  // render while stats are still loading -- keying on those would restart the
  // animation continuously instead of once.
  const barsRef = useAnimate((g, root, opts) => {
    MOTION.grow(g, root.querySelectorAll('.bar-fill'), 'x', 0.04);
  }, [stats, users]);

  return (
    <div ref={barsRef}>
      {/* The accent on each tile comes from the theme, not from a hex written
          here. They used to be literals, which meant the dark theme never reached
          them -- "Resolved" kept the *light* --green at 3.90:1 while its three
          siblings inherited body ink at 14.36:1, so the least prominent number on
          the page was the one reporting something good. #639922 was not one of
          this app's greens at all. Routing all four through tokens lets each page
          follow the theme with no override of its own. */}
      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-label">{t.totalToday}</div>
          <div className="stat-value"><CountUp value={totalIssues}/></div>
          <div className="stat-sub"><span className="stat-dot" style={{background:'var(--blue)'}}></span>{t.aptsAndDepts}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">{t.resolved}</div>
          <div className="stat-value" style={{color:'var(--green)'}}><CountUp value={resolved}/></div>
          <div className="stat-sub"><span className="stat-dot" style={{background:'var(--green)'}}></span>{totalIssues ? Math.round(resolved/totalIssues*100) : 0}{t.resolutionRate}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">{t.avgTime}</div>
          {/* The em dash case gets no unit: "—m" would read as a figure. */}
            <div className="stat-value">{avgNum === null ? avgText : <><CountUp value={avgNum} decimals={1}/><span style={{fontSize:14,fontWeight:400,color:'var(--gray-400)'}}>m</span></>}</div>
          <div className="stat-sub"><span className="stat-dot" style={{background:'var(--amber)'}}></span>{t.acrossAll}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">{t.todayOnShift}</div>
          {/* An EM DASH while the answer is unknown, never 0. "0 people working" is a
              meaningful and wrong answer; the em dash is the one thing on this row that
              is not a figure, which is the same reasoning avgTime gives above. */}
          <div className="stat-value">
            {onNow === null ? '—' : <CountUp value={onNow}/>}
          </div>
          <div className="stat-sub">
            <span className="stat-dot" style={{background: onNow ? 'var(--green)' : 'var(--teal-mid)'}}></span>
            {/* The old tile put EVERY active user's name into this box, which .stat-sub
                has no clamp for -- about 1,800px of text in a 244px column, seven
                wrapped lines, and the page read BROKEN rather than empty. One bounded
                line naming who is NEXT, which is the thing a reader actually wants off
                a dashboard, replaces it.

                The caption is CONDITIONAL rather than falling back to a sentence about
                nobody being on. The first version fell back to todayNoneWorking, which
                printed "2" directly above "Nobody is on shift right now" -- the tile
                contradicting its own caption because the caption was about the wrong
                thing. When nobody is coming later the honest caption is no caption. */}
            {onNext
              ? t.todayNext.replace('{name}', shortName(onNext.name)).replace('{time}', onNext.time)
              : (onNow === 0 ? t.todayNoneWorking : '')}
          </div>
        </div>
      </div>

      {/* The two operational bands sit ABOVE the bar charts, not below them. Everything
          above them is retrospective -- issues logged, issues resolved, averages -- and
          nothing on the page answered "what is happening now" until these two arrived.
          The charts are the only surfaces for by_technician and by_type, so they stay
          rather than go; costing them a scroll is the price of putting the perishable
          content where people are looking. */}
      <TodayBand t={t} lang={lang} rota={rota} onGoToPage={onGoToPage}/>
      <AttentionBand t={t} items={consumableItems} onGoToPage={onGoToPage}/>

      {/* What happened today, after what is true now. The bands above are
          perishable and answer "what needs somebody this minute"; this answers
          "what has the day done so far" and links into the inbox, which is
          where the outstanding items themselves live. */}
      <NotificationDigest digest={digest} unreadCount={notifUnread} showLink
        onOpenInbox={()=>onGoToPage('inbox')} onRetry={onRetryDigest} t={t}/>

      <div className="bottom-grid">
        <div className="mini-card">
          <div className="mini-card-title">{t.workload}</div>
          {byTech.length===0 && <p style={{color:'var(--gray-400)',fontSize:13}}>{t.noUsers}</p>}
          {byTech.map(ts=>(
            <div className="bar-row" key={ts.user_id}>
              <div className="bar-label"><Avatar name={shortName(ts.name)} idx={ts.avatar_idx} size="sm"/>{shortName(ts.name)}</div>
              <div className="bar-track"><div className="bar-fill" style={{width:`${(ts.count/maxWork)*100}%`,background:'var(--blue)'}}></div></div>
              <div className="bar-count">{ts.count}</div>
            </div>
          ))}
        </div>
        <div className="mini-card">
          <div className="mini-card-title">{t.byType}</div>
          {Object.keys(byType).length===0 && <p style={{color:'var(--gray-400)',fontSize:13}}>{t.noIssues}</p>}
          {Object.entries(byType).sort((a,b)=>b[1]-a[1]).map(([type,count])=>(
            <div className="bar-row" key={type}>
              <div className="bar-label">
                <span style={{display:'inline-block',width:10,height:10,borderRadius:2,background:TYPE_COLORS[type]||'var(--gray-400)',flexShrink:0}}></span>
                {type}
              </div>
              <div className="bar-track"><div className="bar-fill" style={{width:`${(count/maxType)*100}%`,background:TYPE_COLORS[type]||'var(--gray-400)'}}></div></div>
              <div className="bar-count">{count}</div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}


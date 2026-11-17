/* ── Notification centre ──
   The server sends a structured kind and parameters, never a sentence. These
   helpers are the only place an item becomes words, which is what keeps both
   languages honest and lets the API stay language-free. */

const NOTIF_PAGE = {
  maintenance_due:   'maintenance',
  maintenance_fault: 'maintenance',
  recurring_alert:   'alerts',
  tv_swap_alert:     'alerts',
  low_stock:         'consumables',
  sla_breach:        'apt',
};

const NOTIF_ICON = {
  maintenance_due:   'tool',
  maintenance_fault: 'tool',
  recurring_alert:   'bell',
  tv_swap_alert:     'tv',
  low_stock:         'box',
  sla_breach:        'clock',
};

// The identity the read mark is written against. source_id alone is not enough:
// the same item can be low against two different reorder levels.
function notifReadKey(item) {
  return item.kind + '|' + item.source_id + '|' + (item.variant || '');
}

function notifPage(item) {
  if (item.kind === 'sla_breach') return item.data?.mode === 'dept' ? 'dept' : 'apt';
  return NOTIF_PAGE[item.kind] || null;
}

function notifDate(value, lang) {
  if (!value) return '';
  const d = new Date(String(value).length === 10 ? value + 'T00:00:00' : value);
  if (isNaN(d)) return '';
  return d.toLocaleDateString(lang === 'pt' ? 'pt-PT' : 'en-GB', {day:'numeric', month:'short'});
}

function notifCopy(item, t, lang) {
  const d = item.data || {};
  switch (item.kind) {
    case 'maintenance_due': {
      const title = t.notifKindMaintDue.replace('{device_name}', d.device_name || '—');
      const equip = d.equipment_type === 'phone' ? t.maintenancePhone : t.maintenancePrinter;
      let due = '';
      if (typeof d.days_until_due === 'number') {
        if (d.days_until_due < 0) due = t.notifMaintDueOverdue.replace('{days}', Math.abs(d.days_until_due));
        else if (d.days_until_due === 0) due = t.notifMaintDueToday;
        else if (d.days_until_due === 1) due = t.notifMaintDueInOne;
        else due = t.notifMaintDueIn.replace('{days}', d.days_until_due);
      } else if (d.due_on) {
        due = t.notifMaintDueOn.replace('{date}', notifDate(d.due_on, lang));
      }
      return {title, detail: [equip, due].filter(Boolean).join(' · ')};
    }
    case 'maintenance_fault': {
      const title = t.notifKindMaintFault.replace('{device_name}', d.device_name || '—');
      const detail = d.note ? d.note : (d.done_on ? t.notifMaintFaultChecked.replace('{date}', notifDate(d.done_on, lang)) : '');
      return {title, detail};
    }
    case 'recurring_alert': {
      const title = t.notifKindRecurring
        .replace('{issue_type}', d.issue_type || '—')
        .replace('{location}', d.location || '—');
      const detail = d.solution ? t.notifRecurringLastFix.replace('{solution}', d.solution) : t.notifRecurringRepeat;
      return {title, detail};
    }
    case 'tv_swap_alert': {
      const title = t.notifKindTvSwap.replace('{from_room}', d.from_room || '—');
      const detail = d.to_room ? t.notifTvSwapDetail.replace('{to_room}', d.to_room) : (d.notes || '');
      return {title, detail};
    }
    case 'low_stock': {
      const title = t.notifKindLowStock.replace('{item_name}', d.item_name || '—');
      const detail = (typeof d.stock === 'number' && d.stock < 0)
        ? t.notifLowStockOverdrawn
            .replace('{count}', Math.abs(d.stock))
            .replace('{reorder_level}', d.reorder_level ?? '—')
        : t.notifLowStockDetail
            .replace('{stock}', d.stock ?? '—')
            .replace('{reorder_level}', d.reorder_level ?? '—');
      return {title, detail};
    }
    case 'sla_breach': {
      const title = t.notifKindSla.replace('{location}', d.location || '—');
      const detail = t.notifSlaDetail
        .replace('{issue_type}', d.issue_type || '—')
        .replace('{minutes_over}', d.minutes_over ?? '—')
        .replace('{sla_minutes}', d.sla_minutes ?? '—');
      return {title, detail};
    }
    default:
      return {title: t.notifKindUnknown, detail: ''};
  }
}

function notifAgo(iso, t, lang, now) {
  const d = new Date(iso);
  if (isNaN(d)) return {text: '', absolute: ''};
  const absolute = d.toLocaleString(lang === 'pt' ? 'pt-PT' : 'en-GB');
  const diff = Math.max(0, now - d.getTime());
  const mins = Math.floor(diff / 60000);
  let text;
  if (mins < 1) {
    text = t.notifTimeNow;
  } else if (mins < 60) {
    text = t.notifTimeMinutes.replace('{count}', mins);
  } else {
    const hours = Math.floor(mins / 60);
    if (hours < 24) {
      text = t.notifTimeHours.replace('{count}', hours);
    } else {
      const days = Math.floor(hours / 24);
      if (days === 1) text = t.notifTimeYesterday;
      else if (days < 7) text = t.notifTimeDays.replace('{count}', days);
      else text = d.toLocaleDateString(lang === 'pt' ? 'pt-PT' : 'en-GB', {day:'numeric', month:'short'});
    }
  }
  return {text, absolute};
}

// A minute tick for the relative times. Paused while the tab is hidden so a
// background tab does not wake up to re-render a list nobody is looking at.
function useNowTick(ms) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => { if (!document.hidden) setNow(Date.now()); }, ms);
    return () => clearInterval(id);
  }, [ms]);
  return now;
}

function NotifIcon({ kind, severity }) {
  const name = NOTIF_ICON[kind] || 'info';
  return <span className={`notif-icon is-${severity || 'info'}`} aria-hidden="true">{Ic[name] || Ic.info}</span>;
}

function NotificationItem({ item, read, busy, now, t, lang, onOpen, onMarkRead }) {
  const copy = notifCopy(item, t, lang);
  const ago = notifAgo(item.occurred_at, t, lang, now);
  const page = notifPage(item);
  const severityLabel = item.severity === 'critical' ? t.notifSeverityCritical
                      : item.severity === 'warning'  ? t.notifSeverityWarning : '';

  const body = (
    <>
      <span className="notif-title">
        {!read && <span className="notif-dot" aria-hidden="true"/>}
        {severityLabel && <span className="sr-only">{severityLabel} </span>}
        {!read && <span className="sr-only">{t.notifUnreadSr} </span>}
        <span className="notif-title-text">{copy.title}</span>
      </span>
      {copy.detail && <span className="notif-detail">{copy.detail}</span>}
      <span className="notif-meta">
        <time dateTime={item.occurred_at} aria-hidden="true">{ago.text}</time>
        <span className="sr-only">{ago.absolute}</span>
      </span>
    </>
  );

  return (
    <li className={`notif-item ${read ? '' : 'is-unread'}`} data-key={notifReadKey(item)}>
      <NotifIcon kind={item.kind} severity={item.severity}/>
      {page ? (
        <button type="button" className="notif-main" onClick={onOpen}>{body}</button>
      ) : (
        <div className="notif-main is-static">{body}</div>
      )}
      <div className="notif-actions">
        {!read && (
          <button type="button" className="notif-mark" disabled={busy} aria-busy={busy}
            aria-label={t.notifMarkReadFor.replace('{title}', copy.title)}
            onClick={onMarkRead}>
            {busy ? '…' : Ic.check}
          </button>
        )}
      </div>
    </li>
  );
}

function NotificationDigest({ digest, unreadCount, showLink, onOpenInbox, onRetry, t }) {
  const lang = React.useContext(LangCtx);
  if (digest === undefined) {
    return (
      <div className="mini-card digest-card" aria-busy="true">
        <div className="digest-head"><h2 className="digest-title">{t.notifDigestTitle}</h2></div>
        <div className="digest-skel"/>
      </div>
    );
  }

  if (digest === null) {
    return (
      <div className="mini-card digest-card">
        <div className="digest-head"><h2 className="digest-title">{t.notifDigestTitle}</h2></div>
        <div className="digest-body">
          <p className="digest-note">
            {Ic.info}<span>{t.notifDigestUnavailable}</span>
            {onRetry && <button className="digest-link" onClick={onRetry}>{t.tryAgain}</button>}
          </p>
        </div>
      </div>
    );
  }

  const tiles = [
    {value: digest.issues?.logged || 0,           label: t.notifDigestLogged},
    {value: digest.issues?.resolved || 0,         label: t.notifDigestResolved},
    {value: digest.maintenance?.faults_found || 0, label: t.notifDigestFaults},
    {value: digest.tv_swaps?.opened || 0,         label: t.notifDigestTvSwaps},
    {value: digest.issues?.open || 0,             label: t.notifDigestOpen},
    {value: digest.maintenance?.due_today || 0,   label: t.notifDigestDueToday},
    {value: digest.maintenance?.overdue || 0,     label: t.notifDigestOverdue},
    {value: digest.stock?.low || 0,               label: t.notifDigestLowStock},
  ];
  const shown = tiles.filter(x => x.value > 0);

  let body;
  if (!digest.has_activity) {
    if (shown.length) {
      // Outstanding work is not activity, so it is named separately rather than
      // dressed up as something that happened today.
      body = (
        <>
          <p className="digest-note">{Ic.check}<span>{t.notifDigestNoActivity}</span></p>
          <div className="digest-grid" style={{marginTop:10}}>
            {shown.map(x => (
              <div className="digest-stat" key={x.label}>
                <div className="digest-stat-value">{x.value}</div>
                <div className="digest-stat-label">{x.label}</div>
              </div>
            ))}
          </div>
        </>
      );
    } else {
      body = <p className="digest-note">{Ic.check}<span>{t.notifDigestQuiet}</span></p>;
    }
  } else if (shown.length) {
    body = (
      <div className="digest-grid">
        {shown.map(x => (
          <div className="digest-stat" key={x.label}>
            <div className="digest-stat-value">{x.value}</div>
            <div className="digest-stat-label">{x.label}</div>
          </div>
        ))}
      </div>
    );
  } else {
    body = <p className="digest-note">{Ic.info}<span>{t.notifDigestActivityElsewhere}</span></p>;
  }

  const asOf = digest.window?.to ? new Date(digest.window.to) : new Date();
  const asOfText = isNaN(asOf) ? '' : asOf.toLocaleTimeString(lang === 'pt' ? 'pt-PT' : 'en-GB', {hour:'2-digit', minute:'2-digit'});

  return (
    <div className="mini-card digest-card">
      <div className="digest-head">
        <h2 className="digest-title">{t.notifDigestTitle}</h2>
        {asOfText && <span className="digest-asof">{t.todayAsOf.replace('{time}', asOfText)}</span>}
      </div>
      <div className="digest-body">{body}</div>
      {(unreadCount > 0 || showLink) && (
        <div className="digest-foot">
          {unreadCount > 0 && <span>{t.notifUnreadOf.replace('{count}', unreadCount)}</span>}
          {showLink && <button className="digest-link" onClick={onOpenInbox}>{t.notifDigestSeeAll}</button>}
        </div>
      )}
    </div>
  );
}

function NotifLoadMore({ list, t }) {
  if (!list.rows) return null;
  const more = !!(list.res && list.res.has_more);
  if (!more) return null;
  if (list.capped) {
    return <div className="loadmore"><span className="loadmore-note">{t.notifCapped}</span></div>;
  }
  return (
    <div className="loadmore">
      <button className="btn btn-ghost" style={{padding:'6px 14px',fontSize:12.5}}
        onClick={list.loadMore} disabled={list.loading}>
        {list.loading ? '…' : t.loadMore}
      </button>
    </div>
  );
}

function NotificationsPage({ token, digest, unreadCount, onUnreadChange, onRetryDigest, setToast, onGoToPage }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [filter, setFilter] = useState('all');
  const [epoch, setEpoch] = useState(0);
  const [readKeys, setReadKeys] = useState(() => new Set());
  const [pendingKey, setPendingKey] = useState('');
  const [markingAll, setMarkingAll] = useState(false);
  const [live, setLive] = useState('');
  const now = useNowTick(60000);
  const lastGood = useRef([]);
  const listRef = useRef(null);

  const paged = usePagedList(limit => `/notifications?limit=${limit}${filter==='unread' ? '&unread=true' : ''}`,
    token, {rowsKey:'notifications', filterKey: `${filter}|${epoch}`});

  if (paged.rows) lastGood.current = paged.rows;
  const data = paged.res;
  // A failed load-more must not blank the rows already on screen, so the last
  // good page is kept beside usePagedList's null.
  const rows = paged.rows || lastGood.current;
  const loadedOnce = paged.rows !== null;

  useEffect(() => {
    if (data && onUnreadChange && typeof data.unread_count === 'number') {
      onUnreadChange(data.unread_count);
    }
  }, [data, onUnreadChange]);

  const isRead = n => n.unread === false || readKeys.has(notifReadKey(n));
  const visible = filter === 'unread' ? rows.filter(n => !isRead(n)) : rows;
  // The prop, not the loaded page's count: marking one read leaves the feed
  // response that produced the list stale, and a chip reading 13 beside a badge
  // reading 12 is the sort of disagreement that makes people distrust both.
  const liveUnread = typeof unreadCount === 'number' ? unreadCount : 0;

  function refresh() { setEpoch(n => n + 1); }

  async function markRead(item) {
    const key = notifReadKey(item);
    if (pendingKey === key || isRead(item)) return;
    setReadKeys(prev => { const next = new Set(prev); next.add(key); return next; });
    setPendingKey(key);
    try {
      const res = await apiFetch('/notifications/read', {
        method: 'POST', token,
        body: {kind: item.kind, source_id: item.source_id, variant: item.variant || ''},
      });
      if (onUnreadChange && typeof res.unread_count === 'number') onUnreadChange(res.unread_count);
      setLive(t.notifReadAnnounce);
    } catch (ex) {
      setReadKeys(prev => { const next = new Set(prev); next.delete(key); return next; });
      setToast(t.notifActionFailed);
    } finally {
      setPendingKey('');
    }
  }

  async function markAll() {
    if (!liveUnread || markingAll) return;
    setMarkingAll(true);
    try {
      const res = await apiFetch('/notifications/read-all', {method:'POST', token});
      if (onUnreadChange && typeof res.unread_count === 'number') onUnreadChange(res.unread_count);
      setLive(t.notifAllReadAnnounce);
      refresh();
    } catch (ex) {
      setToast(t.notifActionFailed);
    } finally {
      setMarkingAll(false);
    }
  }

  function openItem(item) {
    const page = notifPage(item);
    if (!page) return;
    markRead(item);
    onGoToPage(page);
  }

  if (paged.error && !loadedOnce) {
    return (
      <div>
        <div className="table-card notif-intro">
          <p>{t.notifSubtitle}</p>
          <p className="notif-note">{t.notifNote}</p>
        </div>
        <NotificationDigest digest={digest} unreadCount={unreadCount} onRetry={onRetryDigest} t={t}/>
        <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,display:'flex',alignItems:'center',gap:10}}>
          <span style={{flex:1}}>{t.notifError}</span>
          <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}} onClick={refresh}>{t.tryAgain}</button>
        </div>
      </div>
    );
  }

  return (
    <div ref={listRef}>
      <p className="sr-only" role="status" aria-live="polite" aria-atomic="true">{live}</p>

      <div className="table-card notif-intro">
        <p>{t.notifSubtitle}</p>
        <p className="notif-note">{t.notifNote}</p>
      </div>

      <NotificationDigest digest={digest} unreadCount={unreadCount} onRetry={onRetryDigest} t={t}/>

      <div className="notif-toolbar">
        <button className={`chip ${filter==='all'?'active':''}`} aria-pressed={filter==='all'} onClick={()=>setFilter('all')}>
          {t.notifFilterAll}
        </button>
        <button className={`chip ${filter==='unread'?'active':''}`} aria-pressed={filter==='unread'} onClick={()=>setFilter('unread')}>
          {t.notifFilterUnread}{liveUnread > 0 ? ` (${liveUnread})` : ''}
        </button>
        <div className="notif-toolbar-right">
          <button className="btn btn-ghost" style={{padding:'6px 14px',fontSize:12.5}}
            disabled={!liveUnread || markingAll} onClick={markAll}>
            {markingAll ? '…' : t.notifMarkAllRead}
          </button>
        </div>
      </div>

      {paged.error && loadedOnce && (
        <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,marginBottom:12,display:'flex',alignItems:'center',gap:10}}>
          <span style={{flex:1}}>{t.notifError}</span>
          <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}} onClick={refresh}>{t.tryAgain}</button>
        </div>
      )}

      <div className="table-card notif-card">
        <div className="notif-list-head">{t.notifListTitle}</div>
        {!loadedOnce ? (
          <div aria-busy="true">
            <span className="sr-only">{t.notifLoading}</span>
            {[0,1,2,3].map(i => (
              <div className="notif-skel" key={i} aria-hidden="true">
                <div className="notif-skel-line" style={{width:'42%'}}/>
                <div className="notif-skel-line" style={{width:'68%'}}/>
              </div>
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="empty-state">
            {Ic.inbox}
            <p>{filter==='unread' ? t.notifEmptyUnread : t.notifEmptyBody}</p>
            {filter==='unread'
              ? <button className="btn btn-ghost" style={{padding:'6px 14px',fontSize:12.5}} onClick={()=>setFilter('all')}>{t.notifShowAll}</button>
              : <span style={{fontSize:12}}>{t.notifEmptyTitle}</span>}
          </div>
        ) : (
          <ul className="notif-list">
            {visible.map(item => {
              const key = notifReadKey(item);
              return (
                <NotificationItem
                  key={key}
                  item={item}
                  read={isRead(item)}
                  busy={pendingKey === key}
                  now={now}
                  t={t}
                  lang={lang}
                  onOpen={() => openItem(item)}
                  onMarkRead={() => markRead(item)}
                />
              );
            })}
          </ul>
        )}
      </div>

      <NotifLoadMore list={paged} t={t}/>
    </div>
  );
}

function formatSupportDuration(minutes) {
  if (minutes == null) return null;
  const total = Math.round(minutes);
  if (total <= 0) return '0m';
  const days   = Math.floor(total / 1440);
  const hours  = Math.floor((total % 1440) / 60);
  const mins   = total % 60;
  // Compact units: "1h 30m" reads as a turnaround, "30ms" as milliseconds.
  const unit = (n, u) => `${n}${u}`;
  if (days > 0 && hours > 0) return `${unit(days,'d')} ${unit(hours,'h')}`;
  if (days > 0)             return unit(days, 'd');
  if (hours > 0 && mins > 0) return `${unit(hours,'h')} ${unit(mins,'m')}`;
  if (hours > 0)             return unit(hours, 'h');
  return unit(mins, 'm');
}

/* Formats a timestamp for the "Reported" / "Solution" columns. Shows just the
   date when the range spans more than a day, and the time as well when it does
   not, which is what makes a same-day turnaround readable. */
function formatSupportMoment(iso, withTime) {
  if (!iso) return null;
  const d = new Date(iso);
  if (isNaN(d)) return null;
  return withTime
    ? d.toLocaleString(undefined, {day:'2-digit', month:'short', hour:'2-digit', minute:'2-digit'})
    : d.toLocaleDateString(undefined, {day:'2-digit', month:'short', year:'numeric'});
}


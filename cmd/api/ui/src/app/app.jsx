function App() {
  const [lang,    setLang]    = useState(()=>loadLS('lira_lang','en'));
  const [theme,   setTheme]   = useState(()=>loadLS('lira_theme','light'));
  const [session, setSession] = useState(()=>loadLS('lira_session', null)); // {user, token}
  // The two pieces of the inactivity timeout's own state: whether to show the
  // five-minute warning, and whether the sign-in screen owes the user an
  // explanation for why they are back at it.
  const [idleWarning, setIdleWarning]     = useState(false);
  const [signedOutIdle, setSignedOutIdle] = useState(false);
  // PWA state. `offline` is the browser's own online flag, not a failed request:
  // a request can fail for reasons a retry fixes, while offline changes what the
  // app should say before anything is attempted. `updateReady` is a new build
  // installed and waiting to take over.
  const [offline, setOffline] = useState(() => typeof navigator !== 'undefined' && navigator.onLine === false);
  const [updateReady, setUpdateReady] = useState(false);
  const swWaitingRef = useRef(null);
  const swAcceptedRef = useRef(false);
  // The browser's install offer, kept rather than acted on. Chrome and Edge fire
  // `beforeinstallprompt` once, when the origin is secure, the manifest is
  // acceptable and the app is not already installed; the event is the only way to
  // open the prompt later from a button of our own. Null means there is nothing
  // to offer, and the sidebar row is not rendered at all.
  const [installEvent, setInstallEvent] = useState(null);
  // The account the read cache belonged to on the previous render, so a session
  // that ends without an explicit sign-out (an expired token) still wipes it.
  const prevCacheUserRef = useRef(null);
  const [users,   setUsers]   = useState([]);
  // Counts only, straight from the alerts endpoint. The alert lists are the
  // Alerts page's business and are fetched there, a pageful at a time.
  const [alertsPendingCount, setAlertsPendingCount] = useState(0);
  // How many devices are due or overdue, for the nav badge. Counted by the server
  // over the whole active set rather than taken from whatever the Maintenance page
  // last loaded, so the badge and the page can never disagree.
  const [maintenanceDueCount, setMaintenanceDueCount] = useState(0);
  // The inbox's own numbers. `unread` is the sidebar badge and comes back on
  // every feed and digest answer, so the two can never disagree. `digest` is
  // undefined while it has never loaded, null when the request failed, and the
  // object once it has -- three states the card has to tell apart.
  const [notifUnread, setNotifUnread] = useState(0);
  const [notifDigest, setNotifDigest] = useState(undefined);
  // Set while the Maintenance page wants an issue opened on its behalf: the
  // prefill to apply, and what to do once the issue exists. The modal itself stays
  // rendered here, where every other one is.
  const [maintIssue, setMaintIssue] = useState(null);
  // All-time per-member issue totals for the Team page, which used to count them
  // out of every issue App held in memory.
  const [userIssueStats, setUserIssueStats] = useState([]);
  const [issueTypes, setIssueTypes] = useState([]);
  const [departments, setDepartments] = useState([]);
  const [connectaAgents, setConnectaAgents] = useState([]);
  const [consumableItems, setConsumableItems] = useState([]);
  const [settings, setSettings] = useState(null);
  const typeColorMap = useMemo(
    () => Object.fromEntries(issueTypes.map(it => [it.name, it.color])),
    [issueTypes]
  );
  const [stats,   setStats]   = useState(null);
  const [page,    setPage]    = useState('dashboard');
  // A Settings group to open on arrival, set by another page. Cleared once
  // SettingsPage has used it, so coming back to Settings later does not
  // reopen a group the user had closed on purpose.
  const [focusGroup, setFocusGroup] = useState(null);
  // Collapsed by default: the sidebar looks unchanged until it is opened.
  const [miscOpen, setMiscOpen] = useState(false);
  // The phone drawer. Below 860px the sidebar becomes a slide-over; on desktop
  // this state is set and read but the sidebar is always visible anyway.
  const [drawerOpen, setDrawerOpen] = useState(false);
  // How many captured issues are still on this device, for the nav badge. Read
  // from IndexedDB rather than held as the source of truth, because it has to
  // survive a reload and a second tab.
  const [outboxCount, setOutboxCount] = useState(0);
  // Non-null while this user owes an LCU result for today, which is also the
  // condition under which the server refuses everything else.
  const [lcuPrompt, setLcuPrompt] = useState(null);
  // Bumped when the prompt closes, so an open LCU page re-reads its list: the
  // fetch that would have refreshed it was itself refused while the gate was shut.
  const [lcuEpoch, setLcuEpoch] = useState(0);
  const [modal,   setModal]   = useState(null);
  const [editing, setEditing] = useState(null);
  const [pending, setPending] = useState(null);   // issue awaiting delete confirmation
  const [toast,   setToast]   = useState(null);
  const [newIssueId, setNewIssueId] = useState(null);
  const [apiErr,  setApiErr]  = useState('');
  /* Bumped after an issue is created, edited or deleted.
     The issue tables fetch and hold their own rows now, so nothing in App can be
     edited in place to keep them honest. This is the signal they listen on to ask
     the server again -- and it lands in usePagedList's filter key, so they also
     drop back to a single page instead of showing a new row under six pages of
     rows that were all fetched before it existed. */
const [issuesEpoch, setIssuesEpoch] = useState(0);
const bumpIssues = () => setIssuesEpoch(n => n + 1);

  const t     = T[lang];
  const token = session?.token;
  const me    = session?.user;

  /* The digest is also the badge's source: it carries unread_count, so one
     request keeps the sidebar and the card on the same answer. A failure is
     confined to the card (set to null, which renders a retry) and leaves the
     rest of the app alone. */
  function refreshNotifSummary() {
    if (!token) return;
    apiFetch('/notifications/digest', {token})
      .then(res => {
        const d = res.digest || null;
        setNotifDigest(d);
        if (d && typeof d.unread_count === 'number') setNotifUnread(d.unread_count);
      })
      .catch(() => setNotifDigest(null));
  }

  /* Asked for on arrival at the two surfaces that show it, never on a timer.
     The inbox feed already reports the count after every mark, and a poll would
     keep asking for a number nobody is looking at. */
  useEffect(() => {
    if (!token) return;
    if (page === 'dashboard' || page === 'inbox') refreshNotifSummary();
  }, [page, token]);

  useEffect(()=>{
    if ((page === 'settings' || page === 'system') && me?.role !== 'manager') setPage('dashboard');
  }, [page, me]);

  useEffect(()=>{ saveLS('lira_lang', lang); }, [lang]);
  // The document language, which is what a screen reader reads the whole page
  // with. It was pinned to lang="en" in the markup and never moved, so a
  // Portuguese session was pronounced with an English voice from the title down
  // -- the one attribute every other string in the app depends on. Set here,
  // beside the localStorage write, because this effect is the one that already
  // runs on every language change, sign-in screen included.
  useEffect(()=>{ document.documentElement.lang = lang; }, [lang]);
  useEffect(()=>{
    saveLS('lira_theme', theme);
    document.documentElement.classList.toggle('dark', theme === 'dark');
    // Native form controls -- <select> dropdowns, <input type="date"> calendar
    // popups -- are drawn by the OS and do not follow CSS variables. Setting
    // color-scheme on the root element is what tells the browser to render
    // them in the app's own palette rather than the system one. Without this,
    // a dark-mode select opens as a white list with light text on it.
    document.documentElement.style.colorScheme = theme === 'dark' ? 'dark' : 'light';
    // The phone's status bar is painted by the OS from this tag. It has to
    // follow the app's stored theme, which no media query can see.
    const themeMeta = document.getElementById('theme-color-meta');
    if (themeMeta) themeMeta.setAttribute('content', theme === 'dark' ? '#12151A' : '#1769AA');
  }, [theme]);
  useEffect(()=>{
    saveLS('lira_session', session);
    const userID = session && session.user ? session.user.id : null;
    // A session that ends for any reason -- explicit sign-out, an expired token,
    // an account removed -- takes its cached reads with it. The outbox is not
    // cleared: queued work belongs to the account and waits for it to return.
    if (prevCacheUserRef.current != null && userID == null) {
      clearReadCache(prevCacheUserRef.current);
    }
    prevCacheUserRef.current = userID;
    // Namespace the read cache before any request runs. Effects execute in the
    // order they are declared, and the first fetch is two effects below.
    setReadCacheUser(session && session.user);
  }, [session]);

  // Service worker registration. This is what makes the shell open with no
  // network; it is deliberately best-effort, because an unsupported browser or
  // a failed install should leave the online app exactly as it was.
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return;
    let cancelled = false;

    const onControllerChange = () => {
      // Reload only after the user accepted the update. The first install also
      // fires this when the worker claims the page, and reloading then would be
      // a visible flash that accomplished nothing.
      if (swAcceptedRef.current) window.location.reload();
    };

    navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);
    navigator.serviceWorker.register('/sw.js').then(reg => {
      if (cancelled) return;
      if (reg.waiting) {
        swWaitingRef.current = reg.waiting;
        setUpdateReady(true);
      }
      reg.addEventListener('updatefound', () => {
        const installing = reg.installing;
        if (!installing) return;
        installing.addEventListener('statechange', () => {
          // `controller` is the test for "an older page is running". Without it
          // this is a first install, not an update, and there is nothing to
          // replace.
          if (installing.state === 'installed' && navigator.serviceWorker.controller) {
            swWaitingRef.current = reg.waiting;
            setUpdateReady(true);
          }
        });
      });
    }).catch(() => {
      // No worker, no offline shell. The app still works online.
    });

    return () => {
      cancelled = true;
      navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
    };
  }, []);

  // The browser's install offer. `preventDefault` suppresses the browser's own
  // mini-infobar so the offer appears where someone looking for an app icon will
  // look. Both the event and the state are dropped after one use: the prompt
  // cannot be replayed, and a dismissed one must not leave a dead button behind.
  useEffect(() => {
    const standalone = (window.matchMedia && window.matchMedia('(display-mode: standalone)').matches)
      || navigator.standalone === true;
    if (standalone) return;
    const onPrompt = e => {
      e.preventDefault();
      setInstallEvent(e);
    };
    const onInstalled = () => setInstallEvent(null);
    window.addEventListener('beforeinstallprompt', onPrompt);
    window.addEventListener('appinstalled', onInstalled);
    return () => {
      window.removeEventListener('beforeinstallprompt', onPrompt);
      window.removeEventListener('appinstalled', onInstalled);
    };
  }, []);

  // The browser's connection state drives the banner. `online` is only a hint --
  // a connected wifi network with no route to the server still reports online --
  // but the opposite case is reliable and is the one worth warning about.
  useEffect(() => {
    const up   = () => setOffline(false);
    const down = () => setOffline(true);
    window.addEventListener('online', up);
    window.addEventListener('offline', down);
    return () => {
      window.removeEventListener('online', up);
      window.removeEventListener('offline', down);
    };
  }, []);

  // A read that had to use the cache is a stronger signal than navigator.onLine:
  // it covers the hotel proxy that answers 502 while the device still claims to
  // be online. The reverse clears the banner as soon as the server answers again.
  useEffect(() => {
    const onRead = e => {
      if (e.detail && e.detail.cached) setOffline(true);
      else setOffline(false);
    };
    window.addEventListener('lira:read-source', onRead);
    return () => window.removeEventListener('lira:read-source', onRead);
  }, []);

  /* -- The outbox ---------------------------------------------------------
     Captured work lives in IndexedDB, not in this component: closing the app,
     reloading or opening a second tab must not lose it. App only mirrors the
     count for the badge and owns the timer that drains the queue. */

  useEffect(() => {
    if (!me || me.id == null) { setOutboxCount(0); return; }
    let cancelled = false;
    const read = () => outboxAll(me.id).then(rows => {
      if (!cancelled) setOutboxCount(rows.length);
    });
    read();
    // Emitted by every mutation and by a completed sync run.
    window.addEventListener('lira:outbox', read);
    return () => {
      cancelled = true;
      window.removeEventListener('lira:outbox', read);
    };
  }, [me && me.id]);

  useEffect(() => {
    if (!token) return;
    let stopped = false;

    const run = () => { if (!stopped) syncOutbox(session); };

    /* One completed run is worth a toast only when it changed something the
       person can see. A failure is left to the queue page, which shows the row
       and why it is still there; a toast per attempt would be noise. */
    const onResult = e => {
      const r = e.detail && e.detail.sync;
      if (!r) return;
      if (r.sent > 0) {
        setToast(t.toastSynced);
        // A sent issue is a new row and a new number on the dashboard.
        bumpIssues();
        const todayStr = localISODate(new Date());
        apiFetch(`/stats?date=${todayStr}`, {token}).then(res => setStats(res.stats)).catch(() => {});
        apiFetch('/alerts?limit=1', {token}).then(res => setAlertsPendingCount(res.pending_count || 0)).catch(() => {});
      } else if (r.rejected > 0) {
        setToast(t.captureRejectedToast.replace('{count}', String(r.rejected)));
      }
    };

    // On sign-in, when the connection returns, when the capture page asks after
    // queueing something, and once a minute -- the last one covers a hotel proxy
    // that answers 502 while navigator.onLine still says true.
    run();
    window.addEventListener('online', run);
    window.addEventListener('lira:sync', run);
    window.addEventListener('lira:outbox', onResult);
    const timer = setInterval(run, 60 * 1000);

    return () => {
      stopped = true;
      window.removeEventListener('online', run);
      window.removeEventListener('lira:sync', run);
      window.removeEventListener('lira:outbox', onResult);
      clearInterval(timer);
    };
  }, [token, lang]);

  // Warm the room inventory in the read cache even if the capture page is never
  // opened online. Without this, a phone that only ever queues work would reach
  // the capture page offline with no room numbers to choose from -- and a room
  // number cannot be invented, the server checks it against this inventory.
  useEffect(() => {
    if (!token) return;
    apiFetch('/rooms', {token}).catch(() => {});
  }, [token]);

  function applyUpdate() {
    swAcceptedRef.current = true;
    const waiting = swWaitingRef.current;
    if (waiting) waiting.postMessage({type:'SKIP_WAITING'});
    else window.location.reload();
  }

  // Opening the stored install prompt. One `prompt()` call spends the event
  // whatever the answer, so it is cleared first; a refusal is a legitimate
  // answer and gets no banner. Cancelling the browser dialog throws in some
  // builds, which is also not worth reporting.
  async function installApp() {
    const offer = installEvent;
    if (!offer) return;
    setInstallEvent(null);
    try {
      offer.prompt();
      await offer.userChoice;
    } catch (_) {
      // Already used or cancelled: nothing to do.
    }
  }

  // Load all data when logged in.
  //
  // The LCU check goes first and on its own. A reader on trial owes a result for
  // today, and until it is given the server refuses every other route -- so
  // firing the whole batch would come back as a wall of 403s. Asking first means
  // the prompt opens before any of that, and the app is simply not loaded until
  // the answer is in.
  useEffect(() => {
    if (!token) { setLcuPrompt(null); return; }
    let cancelled = false;
    apiFetch('/lcu/today', {token})
      .then(res => {
        if (cancelled) return;
        if (res.required && res.units && res.units.length) {
          setLcuPrompt({units: res.units, today: res.today});
          return;
        }
        setLcuPrompt(null);
        loadEverything();
      })
      .catch(ex => {
        if (cancelled) return;
        if (ex.lcuRequired) {
          setLcuPrompt({units: ex.lcuUnits, today: ex.lcuToday});
          return;
        }
        const message = String((ex && ex.message) || '');
        if (ex.status === 401 || message.includes('401') || message.toLowerCase().includes('token')) {
          setSession(null); // token expired
          return;
        }
        // The gate could not be asked -- offline, or the server is unreachable.
        // Still load what the device has cached: a phone in a plant room should
        // see its last data rather than an empty shell. Writes remain refused by
        // the server, so this cannot be used to bypass the gate.
        loadEverything();
        if (ex.offline || navigator.onLine === false) return;
        setApiErr(message || 'Something went wrong');
      });
    return () => { cancelled = true; };
  }, [token]);

  function loadEverything() {
    const todayStr = localISODate(new Date());
    // Each read feeds a different part of the shell. allSettled rather than all:
    // offline, the reads that have a cached answer succeed and the rest fail,
    // and one missing list should not blank the other nine. The failures are
    // inspected afterwards for the two cases that need more than a banner.
    const reads = {
      users:       apiFetch('/users', {token}),
      stats:       apiFetch(`/stats?date=${todayStr}`, {token}),
      types:       apiFetch('/issue-types', {token}),
      departments: apiFetch('/departments', {token}),
      agents:      apiFetch('/connecta-agents', {token}),
      settings:    apiFetch('/settings', {token}),
      // limit=1: all that is wanted from this call is the two pending counts that
      // drive the sidebar badge and the Analytics sentence. The alerts themselves
      // are fetched by the Alerts page, which pages through them.
      alerts:      apiFetch('/alerts?limit=1', {token}),
      // The consumable catalog in full, because the picker in the issue form needs
      // every item to choose from. The inventory table on the Consumables page
      // asks for its own pageful.
      consumables: apiFetch('/consumable-items?limit=1000', {token}),
      userStats:   apiFetch('/user-issue-stats', {token}),
      // Only the count is wanted from here, for the nav badge. The queue itself is
      // the Maintenance page's, fetched when it is opened.
      maintenance: apiFetch('/maintenance/schedules?recent=0', {token}),
    };
    const names = Object.keys(reads);

    return Promise.allSettled(names.map(name => reads[name])).then(results => {
      const ok = {};
      names.forEach((name, i) => {
        if (results[i].status === 'fulfilled') ok[name] = results[i].value;
      });

      if (ok.users)       setUsers(ok.users.users || []);
      if (ok.stats)       setStats(ok.stats.stats);
      if (ok.types)       setIssueTypes(ok.types.issue_types || []);
      if (ok.departments) setDepartments(ok.departments.departments || []);
      if (ok.agents)      setConnectaAgents(ok.agents.connecta_agents || []);
      if (ok.settings)    setSettings(ok.settings.settings);
      // The counts only. The lists belong to the Alerts page.
      if (ok.alerts)      setAlertsPendingCount(ok.alerts.pending_count || 0);
      if (ok.consumables) setConsumableItems(ok.consumables.consumable_items || []);
      if (ok.userStats)   setUserIssueStats(ok.userStats.stats || []);
      if (ok.maintenance) setMaintenanceDueCount(ok.maintenance.due_count || 0);

      const failures = results
        .filter(r => r.status === 'rejected' && r.reason)
        .map(r => r.reason);
      if (!failures.length) return;

      const gate = failures.find(ex => ex.lcuRequired);
      if (gate) {
        // The gate closed mid-session -- a unit was added, or the day rolled over
        // while the tab sat open. Open the prompt rather than showing an error.
        setLcuPrompt({units: gate.lcuUnits, today: gate.lcuToday});
        return;
      }

      const message = ex => String((ex && ex.message) || '');
      const expired = failures.find(ex => ex.status === 401 || message(ex).includes('401') || message(ex).toLowerCase().includes('token'));
      if (expired) {
        setSession(null); // token expired
        return;
      }

      // Offline is already explained by the banner; repeating "Failed to fetch"
      // once per list that had no cache would bury the one useful sentence.
      if (navigator.onLine === false) return;
      setApiErr(failures[0].message || 'Something went wrong');
    });
  }

  function login(user, token) {
    setSession({user, token});
    setPage('dashboard');
    // Cleared here rather than at sign-out, so a notice can never survive into the
    // next session and greet someone who signed in normally.
    setSignedOutIdle(false);
  }

  /* Signing out tells the server as well as forgetting the token here.

     This used to be local only, which left a row in the tokens table good for
     another seven days -- fine on a laptop, not fine on the shared machine at a
     reception desk, and it would have made the inactivity timeout cosmetic: the
     session would end on screen and still be usable if the token were copied out
     of localStorage.

     The request is best effort. If it fails -- offline, server restarting -- the
     local session still goes, because leaving someone signed in on screen because
     a network call failed is the worse of the two failures. */
  async function logout(reason) {
    // Captured before the session is cleared: clearing the cache needs the
    // account that owned the rows, and setSession only updates the ref on the
    // next render.
    const userID = session && session.user ? session.user.id : readCacheUser;
    try {
      if (token) await apiFetch('/tokens/authentication', {method:'DELETE', token});
    } catch (_) { /* signed out locally regardless */ }

    // The reads go with the session. The outbox does not: queued work belongs to
    // the account and waits for that same account to sign in again.
    clearReadCache(userID);
    setSession(null);
    setIdleWarning(false);
    setSignedOutIdle(reason === 'idle');
    setUsers([]); setStats(null); setIssueTypes([]); setDepartments([]); setConnectaAgents([]); setConsumableItems([]); setSettings(null); setAlertsPendingCount(0); setUserIssueStats([]); setMaintenanceDueCount(0); setMaintIssue(null); setNotifUnread(0); setNotifDigest(undefined);
  }

  useIdleSignOut(!!session, {
    onWarn: setIdleWarning,
    onIdle: () => logout('idle'),
  });

  // Called when profile updates come back from API
  function onSessionUpdate(updatedUser) {
    setSession(s => ({...s, user: updatedUser}));
    saveLS('lira_session', {...session, user: updatedUser});
  }

  // The third-party handovers as the issue endpoint expects them. Fields that
  // don't belong to a handover's company are dropped here so the server never
  // has to reject the request.
  function tvSwapBody(list) {
    return (list || []).map(s => ({
      // id is the server's row id; a swap added in the form has none yet.
      id:        s.id || undefined,
      from_room: s.fromRoom,
      to_room:   s.toRoom,
      notes:     s.notes || '',
    }));
  }

  function supportRequestBody(list) {
    return (list || []).map(s => ({
      id:                  s.id || undefined,
      company:             s.company,
      status:              s.status,
      telnet_technician:   s.company === 'telnet'    ? (s.technician  || null) : null,
      telnet_confirmed_by: s.company === 'telnet'    ? (s.confirmedBy || null) : null,
      telefonica_ticket_id:s.company === 'telefonica' ? (s.ticketId    || null) : null,
      // "YYYY-MM-DDTHH:MM" is exactly what the datetime-local inputs produce.
      started_at:  s.startedAt  || null,
      resolved_at: s.resolvedAt || null,
      notes:       s.notes || '',
    }));
  }

  // A clock time the user never filled in goes to the server as an empty string,
  // not null. The times are *string there and the update path reads the two
  // differently: null means "the client did not mention this field", whereas ""
  // means "there is no time here" and clears the column. A Pending issue has no
  // end time, so it depends on the second one to actually remove it.
  const clockOrEmpty = v => v || '';

  async function addIssue(form, mode) {
    try {
      const body = {
        mode,
        location: mode==='apt' ? form.apt : form.dept,
        type:         form.type,
        problem:      form.problem,
        resolution:   form.resolution,
        time_minutes: Number(form.time),
        start_time:   clockOrEmpty(form.startTime),
        end_time:     clockOrEmpty(form.endTime),
        status:       form.status,
        reported_by_agent:  form.reportedByAgent,
        confirmed_by_agent: form.confirmedByAgent,
        false_positive: !!form.falsePositive,
        consumables: (form.consumables || []).map(c => ({item_id: c.itemId, quantity: c.quantity})),
        support_requests: supportRequestBody(form.supportRequests),
        tv_swaps: tvSwapBody(form.tvSwaps),
      };
      const res = await apiFetch('/issues', {method:'POST', body, token});
      const issue = mapIssue(res.issue);
      // Briefly flag the new row so IssuesTable can animate it in and
      // pulse its status indicator, then clear the flag once that's done. The
      // table picks the row up on its own next time it fetches -- it owns its
      // rows now, so there is no shared array to push into.
      setNewIssueId(issue.id);
      setTimeout(() => setNewIssueId(id => id===issue.id ? null : id), 1200);
      // The table on screen is showing a page fetched before this issue existed.
      // Nudge it so the row the user just created is actually there.
      bumpIssues();
      // Refresh stats
      const todayStr = localISODate(new Date());
      apiFetch(`/stats?date=${todayStr}`, {token}).then(r=>setStats(r.stats)).catch(()=>{});
      setToast(t.toastLogged);
      // Returned so a caller that opened this form on someone's behalf can go on
      // to do something with the issue it just created -- the Maintenance page
      // needs the id to link a check to it.
      return issue.id;
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function saveEdit(form, mode, id) {
    try {
      const body = {
        mode,
        location:     mode==='apt' ? form.apt : form.dept,
        type:         form.type,
        problem:      form.problem,
        resolution:   form.resolution,
        time_minutes: Number(form.time),
        start_time:   clockOrEmpty(form.startTime),
        end_time:     clockOrEmpty(form.endTime),
        status:       form.status,
        reported_by_agent:  form.reportedByAgent,
        confirmed_by_agent: form.confirmedByAgent,
        false_positive: !!form.falsePositive,
        consumables: (form.consumables || []).map(c => ({item_id: c.itemId, quantity: c.quantity})),
        support_requests: supportRequestBody(form.supportRequests),
        tv_swaps: tvSwapBody(form.tvSwaps),
      };
      const res = await apiFetch(`/issues/${id}`, {method:'PATCH', body, token});
      // The tables hold their own rows and refetch on this signal, so an edit
      // shows up in place rather than waiting for someone to reload the page.
      bumpIssues();
      const todayStr = localISODate(new Date());
      apiFetch(`/stats?date=${todayStr}`, {token}).then(r=>setStats(r.stats)).catch(()=>{});
      setToast(t.toastUpdated);
      // Returned for the same reason addIssue returns its id: the issue modal
      // keeps itself open to upload staged photos and must not re-PATCH the
      // issue if that pass is retried.
      return id;
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  // Deleting is two-step: the row's bin button only asks, and the confirmation
  // dialog calls delIssue. `pending` holds {issue, mode} — and busy:true once
  // the request is in flight, so the dialog can't be double-submitted.
  function askDelete(issue, mode) {
    setPending({issue, mode});
  }

  async function delIssue() {
    if (!pending || pending.busy) return;
    const {issue, mode} = pending;
    setPending({issue, mode, busy:true});
    try {
      await apiFetch(`/issues/${issue.id}`, {method:'DELETE', token});
      bumpIssues();
      const todayStr = localISODate(new Date());
      apiFetch(`/stats?date=${todayStr}`, {token}).then(r=>setStats(r.stats)).catch(()=>{});
      setToast(t.toastDeleted);
      setPending(null);
    } catch(ex) {
      setToast('Error: ' + ex.message);
      setPending(null);
    }
  }

    /* The rota is the one page a visitor can read without a session, which is the
       whole point of it: who is on tonight belongs on a noticeboard, not behind a
       login. Everything else here needs a session -- it fetches with the token, it
       shows whose account it is, and every one of its nav entries is gated -- so
       the exception is deliberately narrow: on /schedule with no session, this page
       and a way to sign in, and no sidebar and no other destination.

       Which page the app is on is read from the path rather than kept in state,
       because the shell never mounts for an anonymous visitor so there is no state
       to set. The trailing slashes are stripped and the query string ignored, so a
       crafted URL cannot smuggle the app onto another page. */
    const appPath = (typeof location !== 'undefined' ? location.pathname : '').replace(/\/+$/, '');
    const publicPath = appPath === '/schedule';

    if (!session && !publicPath) return (
      <LangCtx.Provider value={lang}>
        <AuthScreen onLogin={login} lang={lang} onLangChange={setLang} theme={theme} onThemeChange={setTheme}
          notice={signedOutIdle ? t.signedOutIdle : null}/>
      </LangCtx.Provider>
    );

    /* An anonymous visitor on a public page. Deliberately not the shell: a sidebar
       full of links that 403 on click is a worse first impression than no sidebar,
       and this header carries the two things such a reader needs -- what the page
       is, and how to sign in if they intend to change anything. */
    if (!session) return (
      <LangCtx.Provider value={lang}>
        <div className="app">
          <div className="topbar">
            <div className="topbar-title">{t.rotaTitle}</div>
            <div className="topbar-actions">
              {/* A full navigation rather than login(null, null): that call sets the
                  session to a truthy {user:null, token:null}, which would satisfy the
                  guard above and then fail every fetch. Going to the root is also what
                  a reader expects a sign-in link to do. */}
              <a className="btn btn-ghost" href="/">{t.signInBtn}</a>
            </div>
          </div>
          <div className="content" role="main">
            {/* token is undefined here, which is the case the rota API answers with
                can_edit:false. Passing anything else would be inventing an identity
                this visitor does not have. */}
            <SchedulePage token={undefined} setToast={() => {}}/>
          </div>
        </div>
      </LangCtx.Provider>
    );

  // Navigating always closes the phone drawer, including when the destination is
  // the page already showing -- otherwise tapping the current item leaves the
  // drawer over it with no visible response.
  const go = id => { setPage(id); setDrawerOpen(false); };

  const nav = [
    {id:'dashboard', label:t.dashboard,  icon:Ic.grid },
    // The inbox is a personal read state over everything below it, not a third
    // queue of recurring issues, so it sits with the dashboard rather than
    // beside Alerts -- and it carries a tray, not a second bell.
    {id:'inbox',     label:t.notifNav,   icon:Ic.inbox},
    // Capturing is its own destination because it is the phone's way in: the
    // full issue form assumes a connection, this one does not.
    {id:'capture',   label:t.captureNav, icon:Ic.plus },
    {id:'apt',       label:t.aptIssues,  icon:Ic.list },
    {id:'dept',      label:t.deptIssues, icon:Ic.edit },
    {id:'history',   label:t.history,    icon:Ic.clock},
    {id:'alerts',    label:t.alerts,     icon:Ic.bell },
    // Maintenance sits next to Alerts because both are work waiting on somebody,
    // not a record to read back.
    {id:'maintenance', label:t.maintenance, icon:Ic.tool },
    // The rooms destination sits beside Maintenance because it is the same kind
    // of work: a survey of what is in a room and whether it works.
    {id:'roommap',    label:t.roomMapNav, icon:Ic.map },
    {id:'analytics', label:t.analytics,  icon:Ic.trending},
    // The rota is public -- anyone can read it, which is why it sits above
    // the manager-only System and Settings entries rather than beside them.
    {id:'schedule',  label:t.rotaTitle,  icon:Ic.calendar},
    {id:'consumables', label:t.consumables, icon:Ic.box},
    {id:'support',    label:t.supportPage,  icon:Ic.tool},
    {id:'team',      label:t.team,       icon:Ic.users},
    {id:'reports',   label:t.navReports, icon:Ic.report},
    ...(me.role === 'manager' ? [{id:'system', label:t.systemStatus, icon:Ic.server}] : []),
    ...(me.role === 'manager' ? [{id:'settings', label:t.settings, icon:Ic.gear}] : []),
    // A collapsible group rather than a destination: it only reveals its
    // children, so it never takes the .active class itself.
    {id:'misc', label:t.miscellaneous, icon:Ic.grid, group:true, children:[
      {id:'lcu',     label:t.lcuTesting,   icon:Ic.server},
      {id:'tvswaps', label:t.tvSwapsNav,   icon:Ic.tv},
    ]},
    {id:'profile',   label:t.navProfile, icon:Ic.person},
  ];

  // A group's children are not in the top-level list, so the topbar title has to
  // look inside them as well or it would fall back to "Lcu".
  const navLabel = id => {
    for (const n of nav) {
      if (n.id === id) return n.label;
      const child = n.children && n.children.find(c => c.id === id);
      if (child) return child.label;
    }
    return id.charAt(0).toUpperCase() + id.slice(1);
  };

  return (
    <LangCtx.Provider value={lang}>
    <TypeColorCtx.Provider value={typeColorMap}>
      <div className="app">
        <aside className={`sidebar ${drawerOpen ? 'open' : ''}`}>
          <div className="sidebar-logo">
            <div className="logo-mark">{Ic.logo}</div>
            <div><div className="logo-text">LIRA</div><div className="logo-sub">{t.appSub}</div></div>
          </div>
          <div className="sidebar-section">
            <div className="sidebar-section-label">{t.menu}</div>
            {nav.map(n=> n.group ? (
              <div key={n.id}>
                <button
                  className={`nav-item nav-group-toggle ${miscOpen?'open':''} ${n.children.some(c=>c.id===page)?'contains-active':''}`}
                  onClick={()=>setMiscOpen(o=>!o)}
                  aria-expanded={miscOpen}>
                  <span className="nav-group-chevron">{Ic.chevron}</span>
                  {n.icon}
                  <span>{n.label}</span>
                </button>
                {miscOpen && (
                  <div className="nav-group-children">
                    {n.children.map(c=>(
                      <button key={c.id} className={`nav-item nav-item-child ${page===c.id?'active':''}`} onClick={()=>go(c.id)}>
                        {c.icon}
                        <span>{c.label}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            ) : (
              <button key={n.id} className={`nav-item ${page===n.id?'active':''}`} onClick={()=>go(n.id)}>
                {(() => {
                  // Alerts counts outstanding recurring issues; Maintenance counts
                  // devices due or overdue. Different questions, so each nav entry
                  // asks for its own rather than sharing one number. The inbox
                  // counts what this person has not read, which is the only one of
                  // the three that changes by looking at it.
                  const badge = n.id === 'inbox' ? notifUnread
                              : n.id === 'alerts' ? alertsPendingCount
                              : n.id === 'capture' ? outboxCount
                              : n.id === 'maintenance' ? maintenanceDueCount : 0;
                  return badge > 0 ? (
                    <span className="nav-icon-wrap">
                      {n.icon}
                      <span className="nav-badge">{badge > 99 ? '99+' : badge}</span>
                    </span>
                  ) : n.icon;
                })()}
                <span>{n.label}</span>
              </button>
            ))}
          </div>
          <div className="sidebar-footer">
            <button className="sidebar-user" onClick={()=>go('profile')}>
              <Avatar name={shortName(me.name)} idx={me.avatar_idx} src={me.avatar_data}/>
              <div>
                <div className="sidebar-user-name">{shortName(me.name)}</div>
                <div className="sidebar-user-role">{me.role==='manager' ? t.managerRole : t.techRole}</div>
              </div>
            </button>
            {/* Only when the browser has actually offered an install: HTTPS,
                installability met, and not already installed. On iOS, on plain
                HTTP, or in the installed app there is no row at all, rather
                than a button that silently cannot work. */}
            {installEvent && (
              <button className="sidebar-install" onClick={installApp}>
                {Ic.install} <span>{t.installApp}</span>
              </button>
            )}
            <button className="logout-btn" onClick={()=>logout()}>{Ic.logout} {t.signOut}</button>
          </div>
        </aside>

        {/* Only rendered while the drawer is open, which only happens below
            860px. Tapping it closes the drawer; it also stops a stray tap
            landing on the form underneath. */}
        {drawerOpen && (
          <div className="sidebar-scrim" onClick={()=>setDrawerOpen(false)} aria-hidden="true" />
        )}

        <div className="main">
          <div className="topbar">
            <div className="topbar-left">
              <button className="menu-btn" aria-label={t.menu} aria-expanded={drawerOpen}
                onClick={()=>setDrawerOpen(true)}>{Ic.menu}</button>
              <div className="topbar-heading">
                <h1>{navLabel(page)}</h1>
                <p>{today(lang)}</p>
              </div>
            </div>
            <div className="topbar-right">
              <ThemeToggle theme={theme} onChange={setTheme}/>
              <LangToggle lang={lang} onChange={setLang}/>
              {page !== 'profile' && page !== 'reports' && page !== 'admin' && page !== 'lcu' && <>
                <button className="btn btn-ghost" onClick={()=>setModal('dept')}>{Ic.plus} {t.deptIssueBtn}</button>
                <button className="btn btn-primary" onClick={()=>setModal('apt')}>{Ic.plus} {t.aptIssueBtn}</button>
              </>}
            </div>
          </div>

          {apiErr && (
            <div className="page-banner page-banner-error">
              {Ic.info} <span>{apiErr}</span>
              <button className="page-banner-dismiss" onClick={()=>setApiErr('')}>{Ic.x}</button>
            </div>
          )}

          {/* Offline says what still works, not just that something is wrong:
              the shell is cached, reads fall back to the last copy on the
              device, and new issues queue until the connection returns. */}
          {offline && (
            <div className="page-banner page-banner-warning" role="status">
              {Ic.info} <span>{t.offlineBanner}</span>
            </div>
          )}

          {/* A new build is ready but has not taken over. Swapping it in
              silently would reload a form someone is halfway through, so the
              banner offers the moment instead. */}
          {updateReady && (
            <div className="page-banner page-banner-info" role="status">
              {Ic.info} <span>{t.updateReady}</span>
              <button className="page-banner-action" onClick={applyUpdate}>{t.updateNow}</button>
            </div>
          )}

          {/* The five-minute warning. Not a toast: a toast is gone in under three
              seconds, which is not long enough to notice a countdown you have to
              act on. This sits under the topbar until it is dealt with, and any
              movement of the mouse or key takes it away, so the only thing anyone
              has to do about it is keep working. */}
          {idleWarning && (
            <div className="page-banner page-banner-warning" role="status">
              {Ic.clock} <span>{t.idleWarning}</span>
            </div>
          )}

          <div className="content" role="main">
            {/* resetKey={page} so a crash is contained to the page that caused
                it — navigating away clears it and the rest of the app keeps
                working. */}
            <PageErrorBoundary resetKey={page}>
            <PageShell page={page}>
            {page==='dashboard' && <Dashboard users={users} stats={stats}
        consumableItems={consumableItems} onGoToPage={setPage}
        digest={notifDigest} notifUnread={notifUnread} onRetryDigest={refreshNotifSummary}/>}
            {page==='inbox' && <NotificationsPage token={token} digest={notifDigest}
              unreadCount={notifUnread} onUnreadChange={setNotifUnread}
              onRetryDigest={refreshNotifSummary} setToast={setToast} onGoToPage={setPage}/>}
            {page==='capture' && <CapturePage token={token} issueTypes={issueTypes}
              departments={departments} currentUser={me} setToast={setToast}/>}
            {/* The issue tables fetch and paginate themselves. App used to hold
                every issue ever logged and hand the same array to all three of
                these pages at once, which meant the History page needed the whole
                dataset in memory to show one day of it. */}
            {page==='apt'       && <IssuesTable token={token} issueTypes={issueTypes} tableMode="apt"  epoch={issuesEpoch} currentUser={me} newIssueId={newIssueId} slaMinutes={settings?.sla_minutes} onDelete={askDelete} onEdit={(i,m)=>setEditing({issue:i,mode:m})}/>}
            {page==='dept'      && <IssuesTable token={token} issueTypes={issueTypes} tableMode="dept" epoch={issuesEpoch} currentUser={me} newIssueId={newIssueId} slaMinutes={settings?.sla_minutes} onDelete={askDelete} onEdit={(i,m)=>setEditing({issue:i,mode:m})}/>}
            {page==='history'   && <HistoryPage token={token} issueTypes={issueTypes} epoch={issuesEpoch} currentUser={me} slaMinutes={settings?.sla_minutes} onDelete={askDelete} onEdit={(i,m)=>setEditing({issue:i,mode:m})}/>}
            {page==='alerts'    && <AlertsPage currentUser={me} token={token} onCountsUpdate={setAlertsPendingCount} setToast={setToast}/>}
            {page==='maintenance' && <MaintenancePage token={token} currentUser={me} departments={departments} setToast={setToast} onCountsUpdate={setMaintenanceDueCount} onRequestIssue={req => setMaintIssue(req)}/>}
            {page==='roommap' && <RoomMapPage token={token} currentUser={me} setToast={setToast}/>}
            {page==='analytics' && <AnalyticsPage token={token} alertsPendingCount={alertsPendingCount}/>}
            {page==='consumables' && <ConsumablesPage token={token} currentUser={me} onCatalogUpdate={setConsumableItems} setToast={setToast}/>}
            {page==='support' && <SupportPage token={token}/>}
            {page==='team'    && <TeamPage token={token} users={users} userIssueStats={userIssueStats} currentUser={me}
              onUsersUpdate={setUsers} setToast={setToast}/>}
            {page==='reports' && <ReportsPage token={token} slaMinutes={settings?.sla_minutes} issueTypes={issueTypes} theme={theme}/>}
            {/* LCU testing — a card reader on trial for a run of days, the length
                of which a manager sets on the Admin page. The page needs it
                because its copy names the period. */}
            {page==='tvswaps' && <TVSwapsPage token={token}/>}
            {page==='lcu'     && <LCUPage token={token} setToast={setToast}
              windowDays={settings?.lcu_window_days}
              refreshKey={lcuEpoch}
              onLcuRequired={ex=>setLcuPrompt({units:ex.lcuUnits, today:ex.lcuToday})}/>}
            {/* Public, and deliberately not role-gated. A visitor with no token
                reaches this page with token undefined, which is the case the API
                answers with can_edit:false -- the same thing it answers for a
                technician. */}
            {page==='schedule' && <SchedulePage
              token={token}
              setToast={setToast}
              onGoToSettings={id => { setFocusGroup(id); setPage('settings'); }}
            />}
            {page==='system' && me.role==='manager' && <SystemStatusPage token={token}/>}
            {page==='settings' && me.role==='manager' && <SettingsPage
              token={token}
              focusGroup={focusGroup}
              onFocusGroupHandled={() => setFocusGroup(null)}
              issueTypes={issueTypes}
              onTypesUpdate={setIssueTypes}
              departments={departments}
              onDepartmentsUpdate={setDepartments}
              connectaAgents={connectaAgents}
              onConnectaAgentsUpdate={setConnectaAgents}
              consumableItems={consumableItems}
              onConsumableItemsUpdate={setConsumableItems}
              settings={settings}
              onSettingsUpdate={setSettings}
              setToast={setToast}
            />}
            {page==='profile' && <ProfilePage
              currentUser={me}
              token={token}
              onSessionUpdate={onSessionUpdate}
              setToast={setToast}
              lang={lang}
              onLangChange={setLang}
  slaMinutes={settings?.sla_minutes}
            />}
            </PageShell>
            </PageErrorBoundary>
          </div>
        </div>

        {/* The phone's way into the capture form. Hidden on the capture page
            itself, where it would land on the form already on screen. */}
        {page !== 'capture' && (
          <button className="mobile-capture-fab" onClick={()=>go('capture')}
            aria-label={t.captureNav}>
            {Ic.plus}<span>{t.captureNavShort}</span>
          </button>
        )}

        {modal && (
          <IssueModal issueMode={modal} currentUser={me} issueTypes={issueTypes} departments={departments} connectaAgents={connectaAgents} consumableItems={consumableItems} token={token} onClose={()=>setModal(null)} onSave={form=>addIssue(form,modal)} onPhotosChanged={bumpIssues}/>
        )}
        {/* Opened by the Maintenance page when a check found a fault worth logging.
            onLinked runs once the issue exists, with its id, so the check that
            prompted it can point at it. */}
        {maintIssue && <IssueModal
          issueMode="dept"
          prefill={maintIssue.prefill}
          currentUser={me}
          issueTypes={issueTypes}
          departments={departments}
          connectaAgents={connectaAgents}
          consumableItems={consumableItems}
          token={token}
          onClose={()=>setMaintIssue(null)}
          onPhotosChanged={bumpIssues}
          onSave={async form => {
            const id = await addIssue(form, 'dept');
            const linked = maintIssue.onLinked;
            // The modal owns its own close now: it stays open while any photos
            // upload, so unmounting it here would cancel that pass. Linking the
            // maintenance check happens as soon as the issue exists.
            if (id && linked) await linked(id);
            return id;
          }}
        />}
        {lcuPrompt && (
          <LCUPrompt units={lcuPrompt.units} token={token}
            onDone={()=>{
              setLcuPrompt(null); setLcuEpoch(e=>e+1); loadEverything();
              // The gate that refused the queue has just opened. Clear the pause
              // and drain now rather than leaving yesterday's captures waiting
              // for the next minute tick.
              outboxBlockedUntil = 0;
              try { window.dispatchEvent(new CustomEvent('lira:sync')); } catch (_) {}
            }}
            onLogout={()=>logout()}/>
        )}
        {editing && (
          <IssueModal issueMode={editing.mode} editIssue={editing.issue} currentUser={me} issueTypes={issueTypes} departments={departments} connectaAgents={connectaAgents} consumableItems={consumableItems} token={token}
            onClose={()=>setEditing(null)} onSave={form=>saveEdit(form,editing.mode,editing.issue.id)} onPhotosChanged={bumpIssues}/>
        )}
        {pending && (
          <ConfirmDeleteIssue issue={pending.issue} deleting={!!pending.busy}
            onCancel={()=>setPending(null)} onConfirm={delIssue}/>
        )}
        {/* Mounted unconditionally, not behind `toast &&`: the live region has to
            exist before the message does, or screen readers miss the change. */}
        <Toast msg={toast} onDone={()=>setToast(null)}/>
      </div>
    </TypeColorCtx.Provider>
    </LangCtx.Provider>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App/>);

/* ════════════════ TEAM PAGE ════════════════ */
/* Two views over the same roster: the member cards everyone can see, and the
   management table (promote/demote, deactivate/reactivate) that managers only
   get. The management table used to live as a tab on the Profile page, which
   was a strange home for it — administering the team belongs with the team. */
/* ════════════════ RESET CODE DIALOG ════════════════ */
/*
  Shown to a manager immediately after they generate a reset code, and never
  again. The server keeps a bcrypt hash and returns the plaintext exactly once,
  so this dialog is the only place the code will ever be visible — if the manager
  closes it, the fix is another click, not a lookup.
*/
function ResetCodeDialog({ code, userName, expiresIn, onDone, t }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      // navigator.clipboard needs a secure context; the fallback covers a plain
      // http:// host on the hotel's own network, which is where this runs.
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(code);
      } else {
        const ta = document.createElement('textarea');
        ta.value = code;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      setCopied(true);
    } catch(e) {
      // Nothing to recover: the digits are on screen and selectable, so a failed
      // clipboard is not worth interrupting the manager over.
      setCopied(false);
    }
  }

  const minutes = Math.max(1, Math.round((expiresIn || 3600) / 60));

  const panelRef = useDialog({onClose: onDone});

  return (
    <div className="modal-overlay" onClick={e=>e.target===e.currentTarget && onDone()}>
      <div className="modal" style={{maxWidth:430}} ref={panelRef} role="dialog" aria-modal="true"
        aria-label={t.resetCodeTitle.replace('{name}', userName)}>
        <div className="modal-header">
          <div style={{display:'flex',alignItems:'center',gap:10}}>
            <span style={{color:'var(--blue)',display:'flex'}}>{Ic.shield}</span>
            <div>
              <h2>{t.resetCodeTitle.replace('{name}', userName)}</h2>
            </div>
          </div>
        </div>
        <div className="modal-body">
          <div className="reset-code-display">{code}</div>
          <div style={{fontSize:12.5,lineHeight:1.55,color:'var(--gray-600)'}}>
            {t.resetCodeBody}
          </div>
          <div style={{fontSize:12.5,color:'var(--gray-600)'}}>
            {userName} · {minutes} min
          </div>
        </div>
        <div className="modal-footer">
          <button className="btn btn-ghost" onClick={copy}>
            {copied ? <>{Ic.check} {t.resetCodeCopied}</> : t.resetCodeCopy}
          </button>
          <button className="btn btn-primary" onClick={onDone}>
            {t.resetCodeDone}
          </button>
        </div>
      </div>
    </div>
  );
}

function TeamPage({ token, users, userIssueStats, currentUser, onUsersUpdate, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const canManage = currentUser?.role === 'manager';
  const [view, setView] = useState('members');
  // The code a manager has just generated, while its dialog is open.
  const [resetCode, setResetCode] = useState(null);

  // If the signed-in user stops being a manager while the management view is
  // open, drop back to the cards rather than showing controls the API will
  // reject anyway.
  useEffect(() => { if (!canManage) setView('members'); }, [canManage]);

  /* Nothing on this page narrows the roster -- the tabs are drawn from the rows
     themselves -- so this list has no filter key of its own and the retry counter
     below is the only thing that can put the request back in front of the effect. */
  const [retry, setRetry] = useState(0);

  const list = usePagedList(limit => `/users?limit=${limit}`, token, {rowsKey:'users', filterKey: `${retry}`});

  // Per-member totals, aggregated by the server. These used to be counted in the
  // browser out of every issue App had loaded, which only worked while App held
  // all of them: once the issue tables page their results, counting here would
  // quietly under-report anyone with more than a pageful of issues.
  const stats = useMemo(() => {
    const byUser = new Map();
    for (const s of userIssueStats || []) byUser.set(s.user_id, s);
    return byUser;
  }, [userIssueStats]);

  async function changeRole(userId, newRole) {
    try {
      const res = await apiFetch(`/users/${userId}/role`, {method:'PATCH', body:{role: newRole}, token});
      onUsersUpdate(prev => prev.map(u => u.id === userId ? {...u, role: res.user.role} : u));
      setToast('Role updated');
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function toggleActive(userId, active) {
    try {
      const res = await apiFetch(`/users/${userId}/deactivate`, {method:'PATCH', body:{active}, token});
      onUsersUpdate(prev => prev.map(u => u.id === userId ? {...u, active: res.user.active} : u));
      setToast(active ? 'Account reactivated' : 'Account deactivated');
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  /* Asks the server for a reset code for this user. Nothing is chosen here and no
     password is created: the server raises a flag, signs the user out of every
     session, and returns a six-digit code that the manager reads out to them.
     Called with required=false to take the request back, which matters when a
     manager presses it on the wrong person. */
  async function setPasswordReset(userId, required) {
    try {
      const res = await apiFetch(`/users/${userId}/reset-password`, {method:'POST', body:{required}, token});
      onUsersUpdate(prev => prev.map(u => u.id === userId
        ? {...u, must_reset_password: res.user.must_reset_password} : u));
      if (required && res.code) {
        // Only place the code is ever shown, so it is held in component state
        // and shown in a dialog rather than buried in a toast that disappears.
        setResetCode({ code: res.code, name: res.user.name, expiresIn: res.expires_in });
        setToast(t.resetPasswordDone.replace('{name}', shortName(res.user.name)));
      } else {
        setToast(t.resetPasswordCleared.replace('{name}', shortName(res.user.name)));
      }
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  // `shown` rather than `users`: this page fetches and pages its own member list
  // now, so `users` is the whole roster App loaded at start-up and may be a
  // different thing entirely from what is on screen.
  const shown = list.rows || [];

  if (list.error) {
    return (
      <div style={{background:'var(--red-light)',color:'var(--red)',borderRadius:'var(--radius-md)',padding:'10px 16px',fontSize:13,display:'flex',alignItems:'center',gap:10}}>
        <span style={{flex:1}}>{list.error}</span>
        <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
          onClick={()=>setRetry(n => n + 1)}>{t.tryAgain}</button>
      </div>
    );
  }

  if (!list.rows) {
    return (
      <div className="table-card">
        <div className="empty-state">{Ic.users}<p>{t.analyticsLoading}</p></div>
      </div>
    );
  }

  if (!shown.length) {
    return (
      <div className="table-card">
        <div className="empty-state">
          {Ic.users}
          <p>No team members yet.</p>
        </div>
      </div>
    );
  }

  // ── Management view (managers only) ──
  const manageTable = (
    <div className="table-card">
      <div className="table-header">
        <span className="table-title">{t.teamMembers}</span>
      </div>
      <TableScroll label={t.teamMembers}>
        <table>
          <thead>
            <tr>
              <th>{t.name}</th><th>{t.email}</th><th>{t.role}</th>
              <th>{t.status}</th><th>{t.actions}</th>
            </tr>
          </thead>
          <tbody>
            {shown.map(u=>(
              <tr key={u.id} className={u.active ? '' : 'fp-row'}>
                <td style={{minWidth:170}}>
                  <div style={{display:'flex',alignItems:'center',gap:8}}>
                    <Avatar name={shortName(u.name)} idx={u.avatar_idx} size="sm" src={u.avatar_data}/>
                    <span style={{fontWeight:500,whiteSpace:'nowrap'}}>{shortName(u.name)}</span>
                    {u.id===currentUser.id && <span className="team-you">{t.you}</span>}
                  </div>
                </td>
                <td style={{fontSize:13,color:'var(--gray-600)'}}>{u.email}</td>
                <td>
                  <span className={`badge ${u.role==='manager'?'badge-internet':'badge-type'}`}>{u.role}</span>
                </td>
                <td>
                  <span className={`badge ${u.active?'badge-ok':'badge-pending'}`}>
                    {u.active ? t.activeStatus : t.inactive}
                  </span>
                  {/* A pending change is a separate fact from the account being
                      active, so it gets its own badge rather than overloading the
                      status column. */}
                  {u.must_reset_password && (
                    <span className="badge badge-pending" style={{marginLeft:4}}>
                      {t.resetPasswordPending}
                    </span>
                  )}
                </td>
                {/* 250px keeps "Make technician" + "Deactivate" on one line;
                    at 190 they wrapped and made the rows uneven. */}
                <td style={{minWidth:250}}>
                  {/* You cannot demote or deactivate yourself — that is how an
                      account used to lock its manager out of the page. */}
                  {u.id !== currentUser.id ? (
                    <div style={{display:'flex',gap:6,flexWrap:'wrap'}}>
                      <button className="btn btn-ghost" style={{padding:'4px 10px',fontSize:12,whiteSpace:'nowrap'}}
                        onClick={()=>changeRole(u.id, u.role==='manager'?'technician':'manager')}>
                        {u.role==='manager' ? t.makeTechnician : t.makeManager}
                      </button>
                      <button className={`btn ${u.active?'btn-ghost':'btn-primary'}`} style={{padding:'4px 10px',fontSize:12,whiteSpace:'nowrap'}}
                        onClick={()=>toggleActive(u.id, !u.active)}>
                        {u.active ? t.deactivate : t.reactivate}
                      </button>
                      {/* Pending state flips the button to an undo, since a
                          manager who pressed it on the wrong person needs a way
                          back that does not involve a second person agreeing to
                          it. */}
                      <button className="btn btn-ghost" style={{padding:'4px 10px',fontSize:12,whiteSpace:'nowrap'}}
                        onClick={()=>{
                          const ask = u.must_reset_password
                            ? t.resetPasswordUndoConfirm
                            : t.resetPasswordConfirm;
                          if (u.must_reset_password || confirm(ask.replace('{name}', shortName(u.name)))) {
                            setPasswordReset(u.id, !u.must_reset_password);
                          }
                        }}>
                        {u.must_reset_password ? t.resetPasswordUndo : t.resetPassword}
                      </button>
                    </div>
                  ) : <span style={{fontSize:12,color:'var(--gray-300)'}}>—</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </TableScroll>
      <LoadMore list={list} t={t}/>
    </div>
  );

  return (
    <>
      <div className="team-viewbar">
        <div className="team-segmented">
          <button className={view==='members'?'active':''} onClick={()=>setView('members')}>
            {Ic.users}{t.members}
          </button>
          {canManage && (
            <button className={view==='manage'?'active':''} onClick={()=>setView('manage')}>
              {Ic.shield}{t.manage}
            </button>
          )}
        </div>
      </div>

      {resetCode && (
        <ResetCodeDialog code={resetCode.code} userName={shortName(resetCode.name)}
          expiresIn={resetCode.expiresIn} t={t}
          onDone={()=>setResetCode(null)}/>
      )}

      {view==='manage' && canManage ? manageTable : (
    <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fill,minmax(260px,1fr))',gap:16}}>
      {shown.map(u=>{
        const s = stats.get(u.id) || { total: 0, resolved: 0, minutes: 0 };
        // No issues means there is no average to report. Appending "m" to the
        // em dash unconditionally rendered "—m".
        const avg = s.total ? (s.minutes / s.total).toFixed(1) + 'm' : '—';
        const isManager = u.role === 'manager';
        const inactive = u.active === false;

        return (
          <div className={`mini-card${inactive ? ' team-card-inactive' : ''}`} key={u.id}>
            <div className="team-card-head">
              <Avatar name={shortName(u.name)} idx={u.avatar_idx} size="lg" src={u.avatar_data}/>
              <div className="team-card-id">
                <div className="team-card-name">
                  <span>{shortName(u.name)}</span>
                  {u.id===currentUser.id && <span className="team-you">{t.you}</span>}
                  {inactive && <span className="team-inactive-badge">{t.inactive}</span>}
                </div>
                <div className="team-card-role">
                  <span className="team-role-icon">{isManager ? Ic.shield : Ic.tool}</span>
                  <span>{isManager ? t.managerRole : t.techRole}</span>
                </div>
              </div>
            </div>
            <div className="team-card-stats">
              {[[t.issues, s.total], [t.resolved, s.resolved], [t.avg, avg]].map(([l,v])=>(
                <div className="team-stat" key={l}>
                  <div className="team-stat-label">{l}</div>
                  <div className="team-stat-value">{v}</div>
                </div>
              ))}
            </div>
          </div>
        );
      })}
    </div>
      )}

      <div className="table-card" style={{marginTop:16, padding:0, border:'none', background:'transparent', boxShadow:'none'}}>
        <LoadMore list={list} t={t}/>
      </div>
    </>
  );
}


/* ════════════════ PROFILE PAGE ════════════════ */
function ProfilePage({ currentUser, token, onSessionUpdate, setToast, lang, onLangChange, slaMinutes }) {
  const t = T[lang];
  const [myStats,  setMyStats]  = useState(null);
  const [myIssues, setMyIssues] = useState([]);
  const [section,  setSection]  = useState('identity');
  const [saving,   setSaving]   = useState(false);

  // identity form
  const [name,     setName]     = useState(currentUser.name);
  const [email,    setEmail]    = useState(currentUser.email);
  const [pw,       setPw]       = useState('');
  const [pwConf,   setPwConf]   = useState('');
  const [pwErr,    setPwErr]    = useState('');
  const [idErr,    setIdErr]    = useState('');

  // avatar
  const [avatarData, setAvatarData] = useState(currentUser.avatar_data || '');
  const fileRef = useRef();

  useEffect(() => {
    Promise.all([
      apiFetch('/profile/stats',  {token}),
      apiFetch('/profile/issues?limit=10', {token}),
    ]).then(([sRes, iRes]) => {
      setMyStats(sRes.stats);
      setMyIssues((iRes.issues || []).map(mapIssue));
    }).catch(() => {});
  }, [token]);

  async function saveIdentity() {
    setIdErr(''); setSaving(true);
    if (pw && pw !== pwConf) { setPwErr('Passwords do not match.'); setSaving(false); return; }
    try {
      const body = { name, email };
      if (pw) body.password = pw;
      const res = await apiFetch('/profile', {method:'PATCH', body, token});
      onSessionUpdate(res.user);
      setPw(''); setPwConf(''); setPwErr('');
      setToast('Profile updated');
    } catch(ex) { setIdErr(ex.message); }
    finally { setSaving(false); }
  }

  async function saveAvatar(data) {
    try {
      const res = await apiFetch('/profile', {method:'PATCH', body:{avatar_data: data}, token});
      setAvatarData(data);
      onSessionUpdate(res.user);
      setToast('Avatar updated');
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  function handleFileChange(e) {
    const file = e.target.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = ev => {
      // Resize using canvas
      const img = new Image();
      img.onload = () => {
        const canvas = document.createElement('canvas');
        const size = 160;
        canvas.width = size; canvas.height = size;
        const ctx = canvas.getContext('2d');
        const min = Math.min(img.width, img.height);
        const sx = (img.width  - min) / 2;
        const sy = (img.height - min) / 2;
        ctx.drawImage(img, sx, sy, min, min, 0, 0, size, size);
        saveAvatar(canvas.toDataURL('image/jpeg', 0.7));
      };
      img.src = ev.target.result;
    };
    reader.readAsDataURL(file);
  }

  async function saveLang(l) {
    onLangChange(l);
    try {
      await apiFetch('/profile', {method:'PATCH', body:{language: l}, token});
    } catch(ex) {}
  }

  const sections = [
    {id:'identity',    label:'Identity'},
    {id:'avatar',      label:'Avatar'},
    {id:'preferences', label:'Preferences'},
    {id:'activity',    label:'My Activity'},
  ];

  const statCards = myStats ? [
    {label:'Total issues',  value: myStats.total_issues, color:'var(--blue)'},
    {label:'Resolved',      value: myStats.resolved,     color:'var(--green)'},
    {label:'This week',     value: myStats.this_week,    color:'var(--amber)'},
    {label:'Avg. time',     value: myStats.avg_minutes.toFixed(1)+'m', color:'var(--teal)'},
  ] : [];

  return (
    <div style={{maxWidth:720,margin:'0 auto'}}>

      {/* Profile header */}
      <div className="mini-card" style={{display:'flex',alignItems:'center',gap:20,marginBottom:20,padding:'20px 24px'}}>
        <div style={{position:'relative',flexShrink:0}}>
          <Avatar name={shortName(currentUser.name)} idx={currentUser.avatar_idx} size="lg"
            src={avatarData || currentUser.avatar_data}
            style={{width:64,height:64,fontSize:22,border:'2px solid var(--gray-200)'}}/>
          <button onClick={()=>fileRef.current.click()}
            style={{position:'absolute',bottom:0,right:0,width:22,height:22,borderRadius:'50%',
              background:'var(--blue)',border:'2px solid #fff',cursor:'pointer',
              display:'flex',alignItems:'center',justifyContent:'center',padding:0}}>
            <svg width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="white" strokeWidth="2"><path d="M11 2l3 3-8 8H3v-3l8-8z"/></svg>
          </button>
          <input ref={fileRef} type="file" accept="image/*" style={{display:'none'}} onChange={handleFileChange}/>
        </div>
        <div>
          <div style={{fontSize:18,fontWeight:600}}>{shortName(currentUser.name)}</div>
          <div style={{fontSize:13,color:'var(--gray-400)',marginTop:2,display:'flex',alignItems:'center',gap:6}}>
            {currentUser.role==='manager' ? Ic.shield : Ic.tool}
            {currentUser.role==='manager' ? t.managerRole : t.techRole}
          </div>
          <div style={{fontSize:12,color:'var(--gray-400)',marginTop:2}}>{currentUser.email}</div>
        </div>
      </div>

      {/* Sub-nav */}
      <div style={{display:'flex',gap:4,marginBottom:20,borderBottom:'1px solid var(--gray-200)',paddingBottom:0}}>
        {sections.map(s=>(
          <button key={s.id} onClick={()=>setSection(s.id)}
            style={{padding:'8px 14px',border:'none',background:'none',cursor:'pointer',
              fontSize:13,fontWeight:500,fontFamily:'DM Sans,sans-serif',
              color: section===s.id ? 'var(--blue)' : 'var(--gray-600)',
              borderBottom: section===s.id ? '2px solid var(--blue)' : '2px solid transparent',
              marginBottom:-1}}>
            {s.label}
          </button>
        ))}
      </div>

      {/* ── Identity ── */}
      {section==='identity' && (
        <div className="mini-card">
          <div className="mini-card-title" style={{marginBottom:18}}>Personal information</div>
          {idErr && <div className="auth-error" style={{marginBottom:14}}>{idErr}</div>}
          <div style={{display:'flex',flexDirection:'column',gap:14}}>
            <div className="form-row">
              <div className="form-group">
                <label htmlFor="profile-name">Full name</label>
                <input id="profile-name" className="form-control" value={name} onChange={e=>setName(e.target.value)}/>
              </div>
              <div className="form-group">
                <label htmlFor="profile-email">Email address</label>
                <input id="profile-email" className="form-control" type="email" value={email} onChange={e=>setEmail(e.target.value)}/>
              </div>
            </div>
            <div style={{borderTop:'1px solid var(--gray-200)',paddingTop:14}}>
              <div style={{fontSize:13,fontWeight:600,color:'var(--gray-600)',marginBottom:12}}>Change password</div>
              {pwErr && <div style={{color:'var(--red)',fontSize:12,marginBottom:8}}>{pwErr}</div>}
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="profile-pw">New password</label>
                  <input id="profile-pw" className="form-control" type="password" placeholder="Min. 6 characters" value={pw} onChange={e=>{setPw(e.target.value);setPwErr('');}}/>
                </div>
                <div className="form-group">
                  <label htmlFor="profile-pw-confirm">Confirm password</label>
                  <input id="profile-pw-confirm" className="form-control" type="password" placeholder="Repeat new password" value={pwConf} onChange={e=>{setPwConf(e.target.value);setPwErr('');}}/>
                </div>
              </div>
            </div>
            <div style={{display:'flex',justifyContent:'flex-end'}}>
              <button className="btn btn-primary" onClick={saveIdentity} disabled={saving} aria-busy={saving}>
                {Ic.check} {saving ? 'Saving…' : 'Save changes'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Avatar ── */}
      {section==='avatar' && (
        <div className="mini-card">
          <div className="mini-card-title" style={{marginBottom:18}}>Profile picture</div>
          <div style={{display:'flex',flexDirection:'column',alignItems:'center',gap:20,padding:'20px 0'}}>
            {avatarData
              ? <img src={avatarData} style={{width:120,height:120,borderRadius:'50%',objectFit:'cover',border:'3px solid var(--gray-200)'}}/>
              : <div style={{width:120,height:120,borderRadius:'50%',background:'var(--blue-light)',display:'flex',alignItems:'center',justifyContent:'center',fontSize:40,fontWeight:600,color:'var(--blue)'}}>
                  {initials(shortName(currentUser.name))}
                </div>
            }
            <div style={{display:'flex',gap:10}}>
              <button className="btn btn-primary" onClick={()=>fileRef.current.click()}>
                {Ic.upload} Upload photo
              </button>
              {avatarData && (
                <button className="btn btn-ghost" onClick={()=>saveAvatar('')}>
                  Remove photo
                </button>
              )}
            </div>
            <div style={{fontSize:12,color:'var(--gray-400)',textAlign:'center'}}>
              JPG, PNG or GIF. Max 2MB. Will be cropped to a square.
            </div>
          </div>
        </div>
      )}

      {/* ── Preferences ── */}
      {section==='preferences' && (
        <div className="mini-card">
          <div className="mini-card-title" style={{marginBottom:18}}>Preferences</div>
          <div style={{display:'flex',alignItems:'center',justifyContent:'space-between',padding:'12px 0',borderBottom:'1px solid var(--gray-200)'}}>
            <div>
              <div style={{fontSize:14,fontWeight:500}}>Language</div>
              <div style={{fontSize:12,color:'var(--gray-400)',marginTop:2}}>Choose your preferred display language</div>
            </div>
            <LangToggle lang={lang} onChange={saveLang}/>
          </div>
        </div>
      )}

      {/* ── Activity ── */}
      {section==='activity' && (
        <div>
          <div style={{display:'grid',gridTemplateColumns:'repeat(4,1fr)',gap:12,marginBottom:20}}>
            {statCards.map(c=>(
              <div className="stat-card" key={c.label}>
                <div className="stat-label">{c.label}</div>
                <div className="stat-value" style={{color:c.color}}>{c.value}</div>
              </div>
            ))}
          </div>
          <div className="table-card">
            <div className="table-header">
              <span className="table-title">Recent issues</span>
            </div>
            {myIssues.length === 0
              ? <div className="empty-state">{Ic.list}<p>No issues logged yet.</p></div>
              : <table>
                  <thead>
                    <tr>
                      <th>{currentUser.role==='apt' ? 'Apt.' : 'Location'}</th>
                      <th>Type</th><th>Problem</th><th>Status</th><th>Time</th><th>Date</th>
                    </tr>
                  </thead>
                  <tbody>
                    {myIssues.map(i=>(
                      <tr key={i.id}>
                        <td><strong style={{fontFamily:'DM Mono,monospace',fontSize:13}}>{i.apt||i.dept}</strong></td>
                        <td><TypeBadge type={i.type}/></td>
                        <td style={{maxWidth:160,fontSize:13}}>{i.problem}</td>
                        <td><span className={`badge ${i.status==='Ok'?'badge-ok':'badge-pending'}`}>{i.status}</span></td>
                        <td><IssueTime minutes={i.time} sla={slaMinutes} t={t}/></td>
                        <td style={{fontSize:12,color:'var(--gray-400)'}}>{i.createdAt}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
            }
          </div>
        </div>
      )}

    </div>
  );
}


/* ════════════════ AUTH SCREEN ════════════════ */

/* Password field with a show/hide toggle, shared by sign-in, sign-up and the
   forced password change so the interaction is identical in all three.

   This lives at module scope, not inside AuthScreen, and that is load-bearing.
   A component defined inside another component's body is a *new function* on
   every render, so React sees a different element type each time, throws the
   old one away and mounts a fresh one. The input is therefore recreated on
   every keystroke and loses focus after the first character. Keep it here. */
function PwField({ label, id, value, onChange, placeholder, showPw, onToggleShow }) {
  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="pw-wrap">
        <input id={id} className="form-control" type={showPw?'text':'password'}
          placeholder={placeholder} value={value}
          onChange={e=>onChange(e.target.value)} required/>
        <button type="button" className="pw-toggle" onClick={onToggleShow}
          aria-label={showPw ? 'hide password' : 'show password'}>
          {showPw ? Ic.eyeoff : Ic.eye}
        </button>
      </div>
    </div>
  );
}

/* ── inactivity sign-out ──
   Thirty minutes of doing nothing ends the session. The reasons for the shape of
   this are not obvious, so they are written down rather than left to be
   rediscovered:

   The clock is wall-clock, not a tick count. setInterval is throttled hard in a
   background tab -- to once a minute, sometimes less -- so counting ticks would
   keep a session alive for hours in a tab nobody is looking at, which is exactly
   the case this exists for. Comparing Date.now() against the last moment of
   activity gives the same answer whatever the browser does with the timer, and a
   tab coming back into focus finds itself already signed out rather than waiting
   out the remainder of its throttled interval.

   mousemove counts, but only once every two seconds. Someone reading a long page
   is often barely moving, and being signed out for reading is worse than the risk
   it guards against. The throttle is there so a trackpad reporting a pixel of
   drift cannot hold a session open indefinitely.

   Everything else counted is a deliberate act -- a click, a key, a scroll, a tap.
   A password field left half-filled on a reception PC is exactly what this is for,
   which is why the warning below comes first: five minutes of it, and any movement
   takes it away. */
const IDLE_LIMIT_MS      = 30 * 60 * 1000;
const IDLE_WARN_AFTER_MS = 25 * 60 * 1000;
const IDLE_TICK_MS       = 15 * 1000;
const IDLE_MOVE_EVERY_MS = 2000;

function useIdleSignOut(active, { onWarn, onIdle }) {
  const lastRef   = useRef(Date.now());
  const movedRef  = useRef(0);
  const warnedRef = useRef(false);
  // The callbacks are held in a ref so that a caller passing an inline arrow --
  // which is the normal way to write them -- does not restart the timer on every
  // render of the page above this one.
  const cbsRef    = useRef({ onWarn, onIdle });
  useEffect(() => { cbsRef.current = { onWarn, onIdle }; });

  useEffect(() => {
    if (!active) return;

    const dropWarning = () => {
      if (!warnedRef.current) return;
      warnedRef.current = false;
      cbsRef.current.onWarn(false);
    };
    const bump = () => { lastRef.current = Date.now(); dropWarning(); };
    const move = () => {
      const now = Date.now();
      if (now - movedRef.current < IDLE_MOVE_EVERY_MS) return;
      movedRef.current = now;
      bump();
    };

    // No focus, and no visibilitychange. Returning to the tab is not using it:
    // counting either would mean that coming back after an hour away always reset
    // the clock before a throttled background tick could notice, so the timeout
    // could only ever fire while somebody was already watching the screen -- which
    // is the one case it does not exist for. A tab left alone for half an hour is
    // signed out when it is looked at again, which is the correct outcome and the
    // reason the notice on the sign-in screen names the reason.
    const events = ['mousedown', 'keydown', 'wheel', 'touchstart', 'pointerdown', 'scroll'];
    events.forEach(e => window.addEventListener(e, bump, { passive: true }));
    window.addEventListener('mousemove', move, { passive: true });

    const tick = setInterval(() => {
      const idleFor = Date.now() - lastRef.current;
      if (idleFor >= IDLE_LIMIT_MS) { cbsRef.current.onIdle(); return; }
      if (idleFor >= IDLE_WARN_AFTER_MS && !warnedRef.current) {
        warnedRef.current = true;
        cbsRef.current.onWarn(true);
      }
    }, IDLE_TICK_MS);

    return () => {
      clearInterval(tick);
      events.forEach(e => window.removeEventListener(e, bump));
      window.removeEventListener('mousemove', move);
    };
  }, [active]);
}

function AuthScreen({ onLogin, lang, onLangChange, theme, onThemeChange, notice }) {
  const t = T[lang];
  // 'login' | 'register' | 'forgot'
  //
  // 'forgot' is the self-service reset. The code is not emailed: a manager
  // generates it from the Team panel and reads it out, which is what makes this
  // usable for someone who cannot remember their password at all.
  const [mode, setMode]       = useState('login');
  const [email, setEmail]     = useState('');
  const [pw, setPw]           = useState('');
  const [code, setCode]       = useState('');
  const [newPw, setNewPw]     = useState('');
  const [name, setName]       = useState('');
  const [showPw, setShowPw]   = useState(false);
  const [err, setErr]         = useState('');
  const [loading, setLoading] = useState(false);
  // Set when sign-in was refused because a manager asked for a change, so the
  // form can explain itself instead of looking like a dead end.
  const [locked, setLocked]   = useState(false);

  // Shared by every PwField, passed down rather than closed over, so the fields
  // stay a single component identity.
  const pwProps = { showPw, onToggleShow: () => setShowPw(s => !s) };

  async function doLogin(e) {
    e.preventDefault(); setErr(''); setLoading(true);
    try {
      const device = deviceIdentity();
      const data = await apiFetch('/tokens/authentication', {
        method:'POST', body:{ email, password:pw, device_id: device.id, device_name: device.name }
      });
      onLogin(data.user, data.token);
    } catch(ex) {
      // The password was accepted but a manager has asked for a new one, so the
      // server issued nothing. Send them to the reset form with the address
      // already filled in, rather than reporting a failure they cannot act on.
      if (ex.mustResetPassword) {
        setLocked(true);
        setErr('');
        setMode('forgot');
      } else {
        setErr(ex.message==='invalid authentication credentials' ? t.errWrongCreds : ex.message);
      }
    } finally { setLoading(false); }
  }

  async function doReset(e) {
    e.preventDefault(); setErr(''); setLoading(true);
    try {
      const data = await apiFetch('/password-change', {
        method:'POST', body:{ email, code, new_password:newPw }
      });
      // Signed in here: the server hands back a token only once a valid code has
      // been redeemed, and there is no old password left to prove anything with.
      onLogin(data.user, data.token);
    } catch(ex) {
      setErr(ex.message);
    } finally { setLoading(false); }
  }

  async function doRegister(e) {
    e.preventDefault(); setErr(''); setLoading(true);
    try {
      const data = await apiFetch('/users', {
        method:'POST', body:{ name, email, password:pw }
      });
      onLogin(data.user, data.token);
    } catch(ex) {
      setErr(ex.message);
    } finally { setLoading(false); }
  }

  /* ── self-service reset: email + manager's code + new password ── */
  function renderForgot() {
    return (
      <>
        <div className="auth-card-title">{t.forgotTitle}</div>
        <div className="auth-card-sub">{t.forgotSub}</div>
        {locked && (
          <div className="reset-note">{Ic.info} <span>{t.needCodeNote}</span></div>
        )}
        <form onSubmit={doReset}>
          <div className="form-group">
            <label htmlFor="auth-reset-email">{t.email}</label>
            {/* Arriving from a blocked sign-in, the address is already filled in,
                so the cursor belongs on the code instead. Coming in cold from the
                link, it belongs on the address. Exactly one of the two takes
                autoFocus — both cannot. */}
            <input id="auth-reset-email" className="form-control" type="email" placeholder={t.emailPlaceholder}
              value={email} onChange={e=>setEmail(e.target.value)} autoFocus={!locked} required/>
          </div>
          <div className="form-group">
            <label htmlFor="auth-reset-code">{t.resetCodeLabel}</label>
            {/* inputMode numeric and a letter-spaced monospace field: the code is
                six digits and someone is copying it off the back of a manager's
                hand, often one-handed. */}
            <input id="auth-reset-code" className="form-control reset-code-input" inputMode="numeric"
              placeholder="000000" maxLength={6} value={code} autoFocus={locked}
              onChange={e=>setCode(e.target.value.replace(/\D/g,''))}/>
            <div style={{fontSize:12,color:'var(--gray-500)',marginTop:5}}>
              {t.codeFromManager}
            </div>
          </div>
          <PwField id="auth-reset-pw" label={t.newPassword} value={newPw} onChange={setNewPw}
            placeholder="••••••••" {...pwProps}/>
          {newPw && newPw.length < 6 && (
            <div style={{fontSize:12,color:'var(--amber)',marginTop:4}}>{t.passwordTooShort}</div>
          )}
          {err && <div className="auth-error">{err}</div>}
          <button type="submit" className="btn btn-primary btn-full" style={{marginTop:4}}
            disabled={loading || code.length !== 6 || newPw.length < 6}>
            {loading ? '…' : t.changePassword}
          </button>
        </form>
        <button type="button" className="btn btn-ghost btn-full" style={{marginTop:8}}
          onClick={()=>{ setCode(''); setNewPw(''); setErr(''); setLocked(false); setMode('login'); }}>
          {t.backToSignIn}
        </button>
      </>
    );
  }

  function switchMode(m) {
    setMode(m); setErr(''); setEmail(''); setPw(''); setName('');
    setCode(''); setNewPw(''); setLocked(false);
  }

  return (
    <div className="auth-screen">
      <div className="auth-left">
        <div className="auth-brand">
          <div className="auth-logo">
            <div className="auth-logo-mark">{Ic.logo}</div>
            <span className="auth-logo-name">LIRA</span>
          </div>
          <div className="auth-headline">{t.authHeadline.split('\n').map((l,i)=><span key={i}>{l}{i===0&&<br/>}</span>)}</div>
          <div className="auth-sub">{t.authSub}</div>
        </div>
        <div className="auth-features">
          {t.authFeatures.map(f=>(
            <div className="auth-feature" key={f}><span className="auth-feature-dot"></span>{f}</div>
          ))}
        </div>
      </div>

      <div className="auth-right">
        <div className="auth-card">
          <div style={{display:'flex',justifyContent:'flex-end',marginBottom:16,gap:8}}>
            <ThemeToggle theme={theme} onChange={onThemeChange}/>
            <LangToggle lang={lang} onChange={onLangChange}/>
          </div>
          {mode==='forgot' ? renderForgot() : (
          <>
          <div className="auth-card-title">{mode==='login' ? t.welcomeBack : t.createAccount}</div>
          <div className="auth-card-sub">{mode==='login' ? t.signIn : t.joinTeam}</div>

          {/* Why the sign-in form is here when the user did not ask for it. Shown
              on the login step only -- carrying it into "create account" or the
              reset form would be explaining a sign-out they are no longer on the
              path to. */}
          {notice && mode==='login' && (
            <div className="auth-notice" role="status">{Ic.info}<span>{notice}</span></div>
          )}

          <form className="auth-form" onSubmit={mode==='login' ? doLogin : doRegister}>
            {err && <div className="auth-error">{err}</div>}

            {mode==='register' && (
              <div className="form-group">
                <label htmlFor="auth-name">{t.fullName}</label>
                <input id="auth-name" className="form-control" placeholder={t.fullNamePlaceholder} value={name} onChange={e=>setName(e.target.value)} required />
              </div>
            )}
            <div className="form-group">
              <label htmlFor="auth-email">{t.email}</label>
              <input id="auth-email" className="form-control" type="email" placeholder={t.emailPlaceholder} value={email} onChange={e=>setEmail(e.target.value)} required />
            </div>
            <PwField id="auth-pw" label={t.password} value={pw} onChange={setPw}
              placeholder={mode==='register'?t.passwordPlaceholder:'••••••••'} {...pwProps}/>

            <button type="submit" className="btn btn-primary btn-full" style={{marginTop:4}} disabled={loading}>
              {loading ? '…' : (mode==='login' ? t.signInBtn : t.createBtn)}
            </button>
          </form>

          {mode==='login' && (
            <button type="button" className="auth-forgot-link"
              onClick={()=>{ setErr(''); setMode('forgot'); }}>
              {t.forgotPassword}
            </button>
          )}

          <div className="auth-toggle">
            {mode==='login'
              ? <>{t.noAccount} <button onClick={()=>switchMode('register')}>{t.signUp}</button></>
              : <>{t.haveAccount} <button onClick={()=>switchMode('login')}>{t.signInBtn}</button></>}
          </div>
          </>
          )}
        </div>
      </div>
    </div>
  );
}



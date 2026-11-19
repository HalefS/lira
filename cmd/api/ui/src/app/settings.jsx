function previewRoomRun(text) {
  const normalised = String(text || '')
    .trim()
    .replace(/[–—]/g, '-')   // en and em dash
    .replace(/\.\./g, '-')    // "1201..1212"
    .replace(/\bto\b/gi, '-');
  const parts = normalised.split('-');
  const num = part => {
    const p = part.trim();
    return /^[0-9]{1,6}$/.test(p) ? parseInt(p, 10) : null;
  };

  if (parts.length === 1) {
    const n = num(parts[0]);
    return n && n > 0 ? { from: n, to: n, count: 1 } : null;
  }
  if (parts.length !== 2) return null;

  const a = num(parts[0]), b = num(parts[1]);
  if (a === null || b === null || a < 1 || b < 1) return null;

  const from = Math.min(a, b), to = Math.max(a, b);
  return { from, to, count: to - from + 1 };
}

function SettingsPage({ token, focusGroup, onFocusGroupHandled, issueTypes, onTypesUpdate, issuePresets, onIssuePresetsUpdate, departments, onDepartmentsUpdate, connectaAgents, onConnectaAgentsUpdate, consumableItems, onConsumableItemsUpdate, settings, onSettingsUpdate, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [name, setName] = useState('');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  // Whether each catalog list has been opened past its first rows. One flag per
  // list rather than one shared flag, because they are separate columns and
  // opening one has no business revealing another. They only ever go from false
  // to true while the page is mounted, so a rename in progress or an icon editor
  // left open cannot be collapsed out from under itself by a re-render.
  const [typesAll,  setTypesAll]  = useState(false);
  const [deptsAll,  setDeptsAll]  = useState(false);
  const [agentsAll, setAgentsAll] = useState(false);
  const [ciAll,     setCiAll]     = useState(false);

  const [deptName, setDeptName] = useState('');
  // Which department row is open for renaming, and what has been typed into it.
  const [editingDeptId, setEditingDeptId] = useState(null);
  const [deptDraft, setDeptDraft]     = useState('');
  const [savingDept, setSavingDept] = useState(false);
  const [deptErr, setDeptErr] = useState('');

  const [agentName, setAgentName] = useState('');
  const [savingAgent, setSavingAgent] = useState(false);
  const [agentErr, setAgentErr] = useState('');

  const [ciName, setCiName] = useState('');
  const [ciIcon, setCiIcon] = useState('box');
  const [savingCi, setSavingCi] = useState(false);
  const [ciErr, setCiErr] = useState('');
  const [editingIconId, setEditingIconId] = useState(null);

  // Issue presets -- manager-configured starting points for the issue form.
  const [presetTypeID, setPresetTypeID] = useState('');
  const [presetName, setPresetName] = useState('');
  const [presetProblem, setPresetProblem] = useState('');
  const [presetResolution, setPresetResolution] = useState('');
  const [presetConsumables, setPresetConsumables] = useState([]);
  const [editingPresetId, setEditingPresetId] = useState(null);
  const [savingPreset, setSavingPreset] = useState(false);
  const [presetErr, setPresetErr] = useState('');
  const [presetPickerOpen, setPresetPickerOpen] = useState(false);

  // The room inventory. Held here rather than lifted into the app, because the
  // only other thing that needs rooms is the typeahead on the issue form, and that
  // asks the server for matches as it is typed rather than carrying the list.
  const [rooms, setRooms]           = useState([]);
  const [roomTotal, setRoomTotal]   = useState(0);
  const [maxRunSize, setMaxRunSize] = useState(500);
  const [roomRun, setRoomRun]       = useState('');
  const [savingRoom, setSavingRoom] = useState(false);
  const [roomErr, setRoomErr]       = useState('');
  const [roomsAll, setRoomsAll]     = useState(false);

  const loadRooms = useCallback(() => {
    if (!token) return;
    apiFetch('/rooms', {token})
      .then(res => {
        setRooms(res.rooms || []);
        setRoomTotal(res.total_rooms || 0);
        if (res.max_run_size) setMaxRunSize(res.max_run_size);
      })
      .catch(() => {});
  }, [token]);
  useEffect(() => { loadRooms(); }, [loadRooms]);

  // A group named from elsewhere -- the rota's empty state sends a manager here
  // to define their first shift. Set once and cleared, so a later re-render (or a
  // visit that did not ask for anything) cannot reopen a group the user shut.
  useEffect(() => {
    if (!focusGroup) return;
    const g = document.getElementById(focusGroup);
    if (!g) return;
    // Open every group on the way down to the one asked for, or a group that
    // starts collapsed would leave the target's own body clipped.
    let node = g;
    while (node && node !== document.body) {
      if (node.classList && node.classList.contains('set-group') && !node.classList.contains('open')) {
        const head = node.querySelector('.set-group-head');
        if (head) head.click();
      }
      node = node.parentElement;
    }
    g.scrollIntoView({block:'start'});
    onFocusGroupHandled && onFocusGroupHandled();
  }, [focusGroup]);

  const roomPreview = previewRoomRun(roomRun);

  const [windowHours, setWindowHours] = useState(settings?.duplicate_window_hours ?? 24);
  const [lcuDays, setLcuDays] = useState(settings?.lcu_window_days ?? 7);
  // Held as a string, not a number, because it is optional and blank has to stay
  // a state the field can be in. A manager who never set an SLA must be able to
  // clear the box and save, which means sending null; a number state would turn
  // that cleared field into 0 and the server would -- correctly -- reject it.
  const [slaMinutes, setSlaMinutes] = useState(settings?.sla_minutes ?? '');
  const [savingSettings, setSavingSettings] = useState(false);
  useEffect(() => {
    if (settings) {
      setWindowHours(settings.duplicate_window_hours);
      setLcuDays(settings.lcu_window_days ?? 7);
      setSlaMinutes(settings.sla_minutes ?? '');
    }
  }, [settings]);

  // The settings row is written whole, so every send carries every value. What
  // differs is intent: one form owns the alert window, another the LCU trial
  // window, another the SLA, so a saved field never silently carries the others
  // along with it from the editor.
  async function saveSettings(e, which) {
    e.preventDefault();
    setSavingSettings(true);
    try {
      const trimmed = String(slaMinutes).trim();
      const res = await apiFetch('/settings', {
        method:'PATCH',
        body:{
          duplicate_window_hours: Number(windowHours),
          lcu_window_days: Number(lcuDays),
          // null switches the SLA off. A blank field is how that is done.
          sla_minutes: trimmed === '' ? null : Number(trimmed),
        },
        token});
      onSettingsUpdate(res.settings);
      if (which === 'alert') setWindowHours(res.settings.duplicate_window_hours);
      else if (which === 'lcu') setLcuDays(res.settings.lcu_window_days);
      else setSlaMinutes(res.settings.sla_minutes ?? '');
      setToast(t.settingsSaved);
    } catch(ex) { setToast('Error: ' + ex.message); }
    finally { setSavingSettings(false); }
  }

  const saveAlertWindow = e => saveSettings(e, 'alert');
  const saveLcuWindow   = e => saveSettings(e, 'lcu');
  const saveSla         = e => saveSettings(e, 'sla');

  async function addType(e) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setSaving(true); setErr('');
    try {
      const res = await apiFetch('/issue-types', {method:'POST', body:{name: trimmed}, token});
      onTypesUpdate(prev => [...prev, res.issue_type].sort((a,b)=>a.name.localeCompare(b.name)));
      // The list is alphabetical, so a new row can land anywhere in it. Opening it
      // is what stops "I added it and nothing happened" -- without this the new row
      // can be row nine of a list showing five.
      setTypesAll(true);
      setName('');
      setToast(t.toastTypeAdded);
    } catch(ex) { setErr(ex.message); }
    finally { setSaving(false); }
  }

  async function removeType(id) {
    try {
      await apiFetch(`/issue-types/${id}`, {method:'DELETE', token});
      onTypesUpdate(prev => prev.filter(it => it.id !== id));
      setToast(t.toastTypeDeleted);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function updateTypeColor(id, hex) {
    try {
      const res = await apiFetch(`/issue-types/${id}/color`, {method:'PATCH', body:{color:hex}, token});
      onTypesUpdate(prev => prev.map(it => it.id===id ? res.issue_type : it));
      setToast(t.toastTypeColorUpdated);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  // Whether issues of this type offer to record equipment being moved between
  // rooms. Toggling it only changes whether the prompt appears — swaps already
  // recorded stay on their issues either way.
  async function updateTypeTracksSwaps(id, on) {
    try {
      const res = await apiFetch(`/issue-types/${id}/tracks-swaps`, {method:'PATCH', body:{tracks_swaps:on}, token});
      onTypesUpdate(prev => prev.map(it => it.id===id ? res.issue_type : it));
      setToast(on ? t.toastSwapsEnabled : t.toastSwapsDisabled);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function addDept(e) {
    e.preventDefault();
    const trimmed = deptName.trim();
    if (!trimmed) return;
    setSavingDept(true); setDeptErr('');
    try {
      const res = await apiFetch('/departments', {method:'POST', body:{name: trimmed}, token});
      onDepartmentsUpdate(prev => [...prev, res.department].sort((a,b)=>a.name.localeCompare(b.name)));
      setDeptsAll(true);
      setDeptName('');
      setToast(t.toastDeptAdded);
    } catch(ex) { setDeptErr(ex.message); }
    finally { setSavingDept(false); }
  }

  // Renaming edits the row in place rather than opening a dialog: the list
    // already shows the name, so the cell becomes an input and saves from there.
    // A modal for one short field would be heavier than the change being made.
    function startDeptEdit(d) {
      setEditingDeptId(d.id);
      setDeptDraft(d.name);
      setDeptErr('');
    }

    function cancelDeptEdit() {
      setEditingDeptId(null);
      setDeptDraft('');
      setDeptErr('');
    }

    async function saveDeptEdit(id) {
      const trimmed = deptDraft.trim();
      if (!trimmed) { setDeptErr(t.renameDepartmentEmpty); return; }
      setSavingDept(true); setDeptErr('');
      try {
        const res = await apiFetch(`/departments/${id}`, {method:'PATCH', body:{name: trimmed}, token});
        // Replaced rather than edited field-by-field: the API echoes the row back
        // complete, so this is exactly what a fresh load would show.
        onDepartmentsUpdate(prev => prev.map(d => d.id === id ? res.department : d)
          .sort((a,b)=>a.name.localeCompare(b.name)));
        setToast(res.issues_updated > 0
          ? t.toastDeptRenamedWithIssues.replace('{name}', res.department.name)
                                         .replace('{count}', res.issues_updated)
          : t.toastDeptRenamed);
        cancelDeptEdit();
      } catch(ex) { setDeptErr(ex.message); }
      finally { setSavingDept(false); }
    }

    async function removeDept(id) {
    try {
      await apiFetch(`/departments/${id}`, {method:'DELETE', token});
      onDepartmentsUpdate(prev => prev.filter(d => d.id !== id));
      setToast(t.toastDeptDeleted);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function addAgent(e) {
    e.preventDefault();
    const trimmed = agentName.trim();
    if (!trimmed) return;
    setSavingAgent(true); setAgentErr('');
    try {
      const res = await apiFetch('/connecta-agents', {method:'POST', body:{name: trimmed}, token});
      onConnectaAgentsUpdate(prev => [...prev, res.connecta_agent].sort((a,b)=>a.name.localeCompare(b.name)));
      setAgentsAll(true);
      setAgentName('');
      setToast(t.toastAgentAdded);
    } catch(ex) { setAgentErr(ex.message); }
    finally { setSavingAgent(false); }
  }

  async function removeAgent(id) {
    try {
      await apiFetch(`/connecta-agents/${id}`, {method:'DELETE', token});
      onConnectaAgentsUpdate(prev => prev.filter(a => a.id !== id));
      setToast(t.toastAgentDeleted);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function addConsumableItem(e) {
    e.preventDefault();
    const trimmed = ciName.trim();
    if (!trimmed) return;
    setSavingCi(true); setCiErr('');
    try {
      const res = await apiFetch('/consumable-items', {method:'POST', body:{name: trimmed, icon: ciIcon}, token});
      onConsumableItemsUpdate(prev => [...prev, res.consumable_item].sort((a,b)=>a.name.localeCompare(b.name)));
      setCiAll(true);
      setCiName(''); setCiIcon('box');
      setToast(t.toastConsumableAdded);
    } catch(ex) { setCiErr(ex.message); }
    finally { setSavingCi(false); }
  }

  async function removeConsumableItem(id) {
    try {
      await apiFetch(`/consumable-items/${id}`, {method:'DELETE', token});
      onConsumableItemsUpdate(prev => prev.filter(ci => ci.id !== id));
      if (editingIconId === id) setEditingIconId(null);
      setToast(t.toastConsumableDeleted);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function updateConsumableItemIcon(id, icon) {
    try {
      const res = await apiFetch(`/consumable-items/${id}/icon`, {method:'PATCH', body:{icon}, token});
      onConsumableItemsUpdate(prev => prev.map(ci => ci.id===id ? res.consumable_item : ci));
      setEditingIconId(null);
      setToast(t.toastConsumableIconUpdated);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  function presetBody() {
    return {
      issue_type_id: Number(presetTypeID),
      name: presetName.trim(),
      problem: presetProblem.trim(),
      resolution: presetResolution.trim(),
      consumables: presetConsumables.map(c => ({item_id: c.itemId, quantity: c.quantity})),
    };
  }

  function resetPresetForm() {
    setPresetTypeID(''); setPresetName(''); setPresetProblem(''); setPresetResolution('');
    setPresetConsumables([]); setEditingPresetId(null); setPresetErr('');
  }

  function startPresetEdit(preset) {
    setEditingPresetId(preset.id);
    setPresetTypeID(String(preset.issue_type_id || ''));
    setPresetName(preset.name || '');
    setPresetProblem(preset.problem || '');
    setPresetResolution(preset.resolution || '');
    setPresetConsumables((preset.consumables || []).map(c => ({itemId: c.item_id, quantity: c.quantity})));
    setPresetErr('');
  }

  async function savePreset(e) {
    e.preventDefault();
    if (!presetTypeID || !presetName.trim() || !presetProblem.trim()) return;
    setSavingPreset(true); setPresetErr('');
    try {
      const body = presetBody();
      const res = editingPresetId
        ? await apiFetch(`/issue-presets/${editingPresetId}`, {method:'PATCH', body, token})
        : await apiFetch('/issue-presets', {method:'POST', body, token});
      const saved = res.issue_preset;
      onIssuePresetsUpdate(prev => {
        const next = editingPresetId ? prev.map(p => p.id===saved.id ? saved : p) : [...prev, saved];
        return next.sort((a,b) => (a.issue_type||'').localeCompare(b.issue_type||'') || a.name.localeCompare(b.name));
      });
      setToast(editingPresetId ? t.toastPresetUpdated : t.toastPresetAdded);
      resetPresetForm();
    } catch(ex) { setPresetErr(ex.message); }
    finally { setSavingPreset(false); }
  }

  async function removePreset(id) {
    try {
      await apiFetch(`/issue-presets/${id}`, {method:'DELETE', token});
      onIssuePresetsUpdate(prev => prev.filter(p => p.id !== id));
      if (editingPresetId === id) resetPresetForm();
      setToast(t.toastPresetDeleted);
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  async function addRooms(e) {
    e.preventDefault();
    const trimmed = roomRun.trim();
    if (!trimmed) return;
    setSavingRoom(true); setRoomErr('');
    try {
      const res = await apiFetch('/rooms', {method:'POST', body:{run: trimmed}, token});
      setRoomRun('');
      loadRooms();

      // The toast reports what the server did, not what was typed. A range can
      // be partly or wholly already in the inventory now, and the difference is
      // the whole point of the operation: a manager who asked for twenty rooms
      // and is told "rooms added" has no way to tell whether eight were added
      // and twelve were quietly refused, or twenty were added.
      const added   = res.added_rooms   || 0;
      const skipped = res.skipped_rooms || 0;

      if (added === 0) {
        setToast(t.adminRoomsAddedNone.replace('{count}', skipped));
      } else if (skipped === 0) {
        setToast(t.adminRoomsAddedCount.replace('{count}', added));
      } else {
        setToast(t.adminRoomsAddedSkipped
          .replace('{added}', added)
          .replace('{skipped}', skipped));
      }

      // Opened on the way in when something was added, for the same reason the
      // catalog lists are: a range added out of order can land below the fold,
      // and "I added it and nothing happened" is the obvious reading of that.
      // Left collapsed when nothing was added, because there is nothing new to
      // go and look at.
      if (added > 0) setRoomsAll(true);
    } catch(ex) { setRoomErr(ex.message); }
    finally { setSavingRoom(false); }
  }

  async function removeRooms(id) {
    try {
      await apiFetch(`/rooms/${id}`, {method:'DELETE', token});
      setToast(t.adminRoomsDeleted);
      loadRooms();
    } catch(ex) { setToast('Error: ' + ex.message); }
  }

  return (
    <div style={{maxWidth:1160}}>

      {/* ── App configuration ─────────────────────────────────────────
          The three thresholds, together and shut by default. Each is a number
          typed once and then left alone, none of them feeds another, and having
          all three open on arrival put three input boxes above the thing a
          manager actually came here to do. */}
      <SetGroup id="cfg" icon={Ic.clock} title={t.appConfiguration} sub={t.appConfigurationSub}>
        <div className="admin-grid">
          <div className="mini-card">
            <div className="mini-card-title" style={{marginBottom:6}}>{t.adminAlerts}</div>
            <p style={{fontSize:13,color:'var(--gray-400)',margin:'0 0 16px'}}>{t.adminAlertsSub}</p>
            <form onSubmit={saveAlertWindow} className="admin-form">
              <div className="form-group" style={{maxWidth:180}}>
                <label htmlFor="set-dupe-window">{t.duplicateWindowLabel}</label>
                <input id="set-dupe-window" className="form-control" type="number" min="1" max="720" value={windowHours}
                  onChange={e=>setWindowHours(e.target.value)} />
              </div>
              <button type="submit" className="btn btn-primary"
                disabled={savingSettings || !windowHours} aria-busy={savingSettings}>
                {Ic.check} {savingSettings ? '…' : t.saveChanges}
              </button>
            </form>
          </div>

          <div className="mini-card">
            <div className="mini-card-title" style={{marginBottom:6}}>{t.adminLcuWindow}</div>
            <p style={{fontSize:13,color:'var(--gray-400)',margin:'0 0 16px'}}>{t.lcuWindowHint}</p>
            <form onSubmit={saveLcuWindow} className="admin-form">
              <div className="form-group" style={{maxWidth:180}}>
                <label htmlFor="set-lcu-window">{t.lcuWindowLabel}</label>
                <input id="set-lcu-window" className="form-control" type="number" min="1" max="60" value={lcuDays}
                  onChange={e=>setLcuDays(e.target.value)} />
              </div>
              <button type="submit" className="btn btn-primary"
                disabled={savingSettings || !lcuDays} aria-busy={savingSettings}>
                {Ic.check} {savingSettings ? '…' : t.saveChanges}
              </button>
            </form>
          </div>

          <div className="mini-card">
            <div className="mini-card-title" style={{marginBottom:6}}>{t.adminSla}</div>
            <p style={{fontSize:13,color:'var(--gray-400)',margin:'0 0 16px'}}>{t.adminSlaSub}</p>
            <form onSubmit={saveSla} className="admin-form">
              <div className="form-group" style={{maxWidth:180}}>
                <label htmlFor="set-sla">{t.slaMinutesLabel}</label>
                {/* No placeholder number on purpose: a greyed-out 30 would be read as
                    a real threshold by anyone who skimmed it, and the difference
                    between "30 minutes" and "no SLA" is exactly what this field has
                    to make obvious. */}
                <input id="set-sla" className="form-control" type="number" min="1" max="14400"
                  value={slaMinutes} placeholder={t.slaUnsetPlaceholder}
                  onChange={e=>setSlaMinutes(e.target.value)} />
              </div>
              <button type="submit" className="btn btn-primary"
                disabled={savingSettings || String(slaMinutes).trim() === ''} aria-busy={savingSettings}>
                {Ic.check} {savingSettings ? '…' : t.saveChanges}
              </button>
            </form>
          </div>
        </div>
      </SetGroup>

      {/* ── Catalogs ──────────────────────────────────────────────────
          Open, because this is where the work is. Each catalog is a row of its
          own rather than a stack of two cards, so its name is stated once
          instead of twice and the five of them read as a list instead of as a
          wall. All five start shut: a collapsed header still carries the name,
          the explanation and the row count, so nothing is hidden -- it is just
          not open. Whichever ones do get opened stay open next time, which is
          the other half of not having to set the page up on every visit. */}
      <SetGroup id="cat" icon={Ic.grid} title={t.catalogManagement} sub={t.catalogManagementSub} defaultOpen>

        <SetGroup id="cat-types" level={1} title={t.adminIssueTypes} sub={t.adminIssueTypesSub}
          count={issueTypes.length}>
          <div className="set-body">
            <div className="set-form">
              {err && <div className="auth-error" style={{marginBottom:14}}>{err}</div>}
              <form onSubmit={addType} className="admin-form">
                <div className="form-group">
                  <label htmlFor="set-type-name">{t.issueType}</label>
                  <input id="set-type-name" className="form-control" placeholder={t.issueTypePlaceholder} value={name}
                    onChange={e=>setName(e.target.value)} maxLength={50}/>
                </div>
                <button type="submit" className="btn btn-primary" disabled={saving || !name.trim()} aria-busy={saving}>
                  {Ic.plus} {saving ? '…' : t.addIssueType}
                </button>
              </form>
            </div>

            <div className="table-card">
              {issueTypes.length === 0
                ? <div className="empty-state">{Ic.list}<p>{t.noIssueTypes}</p></div>
                : <table>
                    <thead>
                      <tr><th>{t.type}</th><th style={{width:130}}>{t.adminSwapsCol}</th><th></th></tr>
                    </thead>
                    <tbody>
                      {issueTypes.slice(0, typesAll ? issueTypes.length : ADMIN_PREVIEW_ROWS).map(it=>(
                        <tr key={it.id}>
                          <td><TypeBadge type={it.name}/></td>
                          {/* Turn the "record equipment swaps" prompt on for this
                              type. A checkbox rather than a button because the
                              state it shows is the state it sets. */}
                          <td>
                            <label className="swap-toggle" title={t.adminSwapsHint}>
                              <input type="checkbox" checked={!!it.tracks_swaps}
                                onChange={e=>updateTypeTracksSwaps(it.id, e.target.checked)}/>
                              <span>{it.tracks_swaps ? t.adminSwapsOn : t.adminSwapsOff}</span>
                            </label>
                          </td>
                          <td style={{textAlign:'right'}}>
                            <input type="color" className="color-swatch-input" title={t.changeColor}
                              aria-label={`${t.changeColor} — ${it.name}`}
                              value={it.color} onChange={e=>updateTypeColor(it.id, e.target.value)}/>
                            <button className="icon-btn" title={t.removeType} onClick={()=>removeType(it.id)}>{Ic.trash}</button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
              }
              <AdminShowMore hidden={typesAll ? 0 : Math.max(0, issueTypes.length - ADMIN_PREVIEW_ROWS)}
                label={t.adminShowMore} onClick={()=>setTypesAll(true)}/>
            </div>
          </div>
        </SetGroup>

        <SetGroup id="cat-depts" level={1} title={t.adminDepartments} sub={t.adminDepartmentsSub}
          count={departments.length}>
          <div className="set-body">
            <div className="set-form">
              {deptErr && <div className="auth-error" style={{marginBottom:14}}>{deptErr}</div>}
              <form onSubmit={addDept} className="admin-form">
                <div className="form-group">
                  <label htmlFor="set-dept-name">{t.department}</label>
                  <input id="set-dept-name" className="form-control" placeholder={t.departmentPlaceholder} value={deptName}
                    onChange={e=>setDeptName(e.target.value)} maxLength={50}/>
                </div>
                <button type="submit" className="btn btn-primary" disabled={savingDept || !deptName.trim()} aria-busy={savingDept}>
                  {Ic.plus} {savingDept ? '…' : t.addDepartment}
                </button>
              </form>
            </div>

            <div className="table-card">
              {departments.length === 0
                ? <div className="empty-state">{Ic.list}<p>{t.noDepartments}</p></div>
                : <table>
                    <thead>
                      <tr><th>{t.department}</th><th></th></tr>
                    </thead>
                    <tbody>
                      {departments.slice(0, deptsAll ? departments.length : ADMIN_PREVIEW_ROWS).map(d => (
                        <tr key={d.id}>
                          {editingDeptId === d.id ? (
                            <td>
                              <input className="form-control" autoFocus maxLength={50}
                                aria-label={`${t.renameDepartment} — ${d.name}`}
                                value={deptDraft}
                                onChange={e=>setDeptDraft(e.target.value)}
                                onKeyDown={e=>{
                                  if (e.key === 'Enter') saveDeptEdit(d.id);
                                  if (e.key === 'Escape') cancelDeptEdit();
                                }}/>
                              {deptErr && <div style={{fontSize:12,color:'var(--red)',marginTop:5}}>{deptErr}</div>}
                            </td>
                          ) : (
                            <td>{d.name}</td>
                          )}
                          <td style={{textAlign:'right',whiteSpace:'nowrap'}}>
                            {editingDeptId === d.id ? (
                              <>
                                <button className="btn btn-primary" style={{padding:'4px 12px',fontSize:12}}
                                  onClick={()=>saveDeptEdit(d.id)} disabled={savingDept || !deptDraft.trim()} aria-busy={savingDept}>
                                  {savingDept ? '…' : t.saveChanges}
                                </button>
                                <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
                                  onClick={cancelDeptEdit} disabled={savingDept}>{t.cancel}</button>
                              </>
                            ) : (
                              <>
                                <button className="icon-btn" title={t.renameDepartment}
                                  onClick={()=>startDeptEdit(d)}>{Ic.edit}</button>
                                <button className="icon-btn" title={t.removeDepartment}
                                  onClick={()=>removeDept(d.id)}>{Ic.trash}</button>
                              </>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
              }
              <AdminShowMore hidden={deptsAll ? 0 : Math.max(0, departments.length - ADMIN_PREVIEW_ROWS)}
                label={t.adminShowMore} onClick={()=>setDeptsAll(true)}/>
            </div>
          </div>
        </SetGroup>

        <SetGroup id="cat-agents" level={1} title={t.adminAgents} sub={t.adminAgentsSub}
          count={connectaAgents.length}>
          <div className="set-body">
            <div className="set-form">
              {agentErr && <div className="auth-error" style={{marginBottom:14}}>{agentErr}</div>}
              <form onSubmit={addAgent} className="admin-form">
                <div className="form-group">
                  <label htmlFor="set-agent-name">{t.connectaAgent}</label>
                  <input id="set-agent-name" className="form-control" placeholder={t.agentPlaceholder} value={agentName}
                    onChange={e=>setAgentName(e.target.value)} maxLength={100}/>
                </div>
                <button type="submit" className="btn btn-primary" disabled={savingAgent || !agentName.trim()} aria-busy={savingAgent}>
                  {Ic.plus} {savingAgent ? '…' : t.addAgent}
                </button>
              </form>
            </div>

            <div className="table-card">
              {connectaAgents.length === 0
                ? <div className="empty-state">{Ic.list}<p>{t.noAgents}</p></div>
                : <table>
                    <thead>
                      <tr><th>{t.connectaAgent}</th><th></th></tr>
                    </thead>
                    <tbody>
                      {connectaAgents.slice(0, agentsAll ? connectaAgents.length : ADMIN_PREVIEW_ROWS).map(a=>(
                        <tr key={a.id}>
                          <td>{a.name}</td>
                          <td style={{textAlign:'right'}}>
                            <button className="icon-btn" title={t.removeAgent} onClick={()=>removeAgent(a.id)}>{Ic.trash}</button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
              }
              <AdminShowMore hidden={agentsAll ? 0 : Math.max(0, connectaAgents.length - ADMIN_PREVIEW_ROWS)}
                label={t.adminShowMore} onClick={()=>setAgentsAll(true)}/>
            </div>
          </div>
        </SetGroup>

        <SetGroup id="cat-consumables" level={1} title={t.adminConsumables} sub={t.adminConsumablesSub}
          count={consumableItems.length}>
          <div className="set-body">
            <div className="set-form">
              {ciErr && <div className="auth-error" style={{marginBottom:14}}>{ciErr}</div>}
              <form onSubmit={addConsumableItem}>
                <div className="form-group">
                  <label htmlFor="set-consumable-name">{t.consumableName}</label>
                  <input id="set-consumable-name" className="form-control" placeholder={t.consumableNamePlaceholder} value={ciName}
                    onChange={e=>setCiName(e.target.value)} maxLength={50}/>
                </div>
                <div className="form-group" style={{marginTop:14}}>
                  {/* A caption over a grid of choices, not a label for one of them. */}
                  <div className="form-group-h" id="set-consumable-icon-h">{t.consumableIconLabel}</div>
                  <ConsumableIconGrid labelledBy="set-consumable-icon-h" value={ciIcon} onChange={setCiIcon}/>
                </div>
                <div className="admin-form-inline" style={{marginTop:16}}>
                  <div className="ci-preview">
                    <ConsumableIcon name={ciIcon} size={36}/>
                    <span style={{color: ciName.trim() ? 'var(--gray-900)' : 'var(--gray-400)'}}>{ciName.trim() || t.consumableNamePlaceholder}</span>
                  </div>
                  <button type="submit" className="btn btn-primary" disabled={savingCi || !ciName.trim()} aria-busy={savingCi}>
                    {Ic.plus} {savingCi ? '…' : t.addConsumable}
                  </button>
                </div>
              </form>
            </div>

            <div className="table-card">
              {consumableItems.length === 0
                ? <div className="empty-state">{Ic.box}<p>{t.noConsumableItems}</p></div>
                : <table>
                    <thead>
                      <tr><th>{t.consumableName}</th><th></th></tr>
                    </thead>
                    <tbody>
                      {consumableItems.slice(0, ciAll ? consumableItems.length : ADMIN_PREVIEW_ROWS).map(ci=>(
                        <React.Fragment key={ci.id}>
                          <tr>
                            <td>
                              <div style={{display:'flex',alignItems:'center',gap:10}}>
                                <button type="button" className="cicon-btn" title={t.changeIcon}
                                  onClick={()=>setEditingIconId(editingIconId===ci.id ? null : ci.id)}>
                                  <ConsumableIcon name={ci.icon} size={34}/>
                                </button>
                                <span style={{fontWeight:500}}>{ci.name}</span>
                              </div>
                            </td>
                            <td style={{textAlign:'right'}}>
                              <button className="icon-btn" title={t.removeConsumable} onClick={()=>removeConsumableItem(ci.id)}>{Ic.trash}</button>
                            </td>
                          </tr>
                          {editingIconId === ci.id && (
                            <tr className="icon-editor-row">
                              <td colSpan={2}>
                                <div className="icon-editor-label">{t.changeIcon}</div>
                                <ConsumableIconGrid value={ci.icon} onChange={icon=>updateConsumableItemIcon(ci.id, icon)}/>
                              </td>
                            </tr>
                          )}
                        </React.Fragment>
                      ))}
                    </tbody>
                  </table>
              }
              <AdminShowMore hidden={ciAll ? 0 : Math.max(0, consumableItems.length - ADMIN_PREVIEW_ROWS)}
                label={t.adminShowMore} onClick={()=>setCiAll(true)}/>
            </div>
          </div>
        </SetGroup>

        <SetGroup id="cat-presets" level={1} title={t.adminIssuePresets} sub={t.adminIssuePresetsSub}
          count={issuePresets.length}>
          <div className="set-body">
            <div className="set-form">
              {presetErr && <div className="auth-error" style={{marginBottom:14}} role="alert">{presetErr}</div>}
              <form onSubmit={savePreset}>
                <div className="form-group">
                  <label htmlFor="set-preset-type">{t.issueType}</label>
                  <select id="set-preset-type" className="form-control" value={presetTypeID}
                    onChange={e=>setPresetTypeID(e.target.value)}>
                    <option value="">{t.presetSelectType}</option>
                    {issueTypes.map(it=><option key={it.id} value={it.id}>{it.name}</option>)}
                  </select>
                </div>
                <div className="form-group" style={{marginTop:14}}>
                  <label htmlFor="set-preset-name">{t.presetName}</label>
                  <input id="set-preset-name" className="form-control" placeholder={t.presetNamePlaceholder}
                    value={presetName} onChange={e=>setPresetName(e.target.value)} maxLength={100}/>
                </div>
                <div className="form-group" style={{marginTop:14}}>
                  <label htmlFor="set-preset-problem">{t.problemDesc}</label>
                  <input id="set-preset-problem" className="form-control" placeholder={t.presetProblemPlaceholder}
                    value={presetProblem} onChange={e=>setPresetProblem(e.target.value)} maxLength={500}/>
                </div>
                <div className="form-group" style={{marginTop:14}}>
                  <label htmlFor="set-preset-resolution">{t.resolutionLabel}</label>
                  <input id="set-preset-resolution" className="form-control" placeholder={t.presetResolutionPlaceholder}
                    value={presetResolution} onChange={e=>setPresetResolution(e.target.value)} maxLength={500}/>
                </div>
                <div className="form-group" style={{marginTop:14}}>
                  <div className="form-group-h" id="set-preset-consumables-h">{t.consumablesLabel}</div>
                  <div className={`consumable-box ${presetConsumables.length ? 'has-items' : ''}`}
                       role="group" aria-labelledby="set-preset-consumables-h">
                    {presetConsumables.map(c=>{
                      const item = consumableItems.find(ci=>ci.id===c.itemId);
                      if (!item) return null;
                      return (
                        <span className="consumable-chip" key={c.itemId}>
                          <ConsumableIcon name={item.icon} size={22}/>
                          <span className="consumable-chip-name">{item.name}</span>
                          {c.quantity > 1 && <span className="consumable-chip-qty">A-{c.quantity}</span>}
                          <button type="button" className="consumable-chip-x" aria-label={t.consumableRemove}
                            onClick={()=>setPresetConsumables(prev=>prev.filter(x=>x.itemId!==c.itemId))}>{Ic.x}</button>
                        </span>
                      );
                    })}
                    <button type="button" className="consumable-add-btn" onClick={()=>setPresetPickerOpen(true)}>
                      {presetConsumables.length ? Ic.edit : Ic.plus}
                      {presetConsumables.length ? t.editConsumables : t.addConsumables}
                    </button>
                  </div>
                </div>
                <div className="admin-form-inline" style={{marginTop:16}}>
                  <button type="submit" className="btn btn-primary"
                    disabled={savingPreset || !presetTypeID || !presetName.trim() || !presetProblem.trim()}
                    aria-busy={savingPreset}>
                    {Ic.check} {savingPreset ? '…' : (editingPresetId ? t.saveChanges : t.addPreset)}
                  </button>
                  {editingPresetId && (
                    <button type="button" className="btn btn-ghost" onClick={resetPresetForm} disabled={savingPreset}>
                      {t.cancel}
                    </button>
                  )}
                </div>
              </form>
            </div>

            <div className="table-card">
              {issuePresets.length === 0
                ? <div className="empty-state">{Ic.list}<p>{t.noIssuePresets}</p></div>
                : <table>
                    <thead>
                      <tr><th>{t.issueType}</th><th>{t.presetName}</th><th>{t.problemDesc}</th><th>{t.consumablesLabel}</th><th></th></tr>
                    </thead>
                    <tbody>
                      {issuePresets.map(preset=>(
                        <tr key={preset.id}>
                          <td><TypeBadge type={preset.issue_type}/></td>
                          <td style={{fontWeight:500}}>{preset.name}</td>
                          <td style={{maxWidth:360,overflow:'hidden',textOverflow:'ellipsis',whiteSpace:'nowrap'}}>{preset.problem}</td>
                          <td>{(preset.consumables || []).length}</td>
                          <td style={{textAlign:'right',whiteSpace:'nowrap'}}>
                            <button className="icon-btn" title={t.editPreset} onClick={()=>startPresetEdit(preset)}>{Ic.edit}</button>
                            <button className="icon-btn" title={t.removePreset} onClick={()=>removePreset(preset.id)}>{Ic.trash}</button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
              }
            </div>
          </div>
        </SetGroup>

        {/* Rooms, grouped with the others because it is the same kind of thing: a
            list a manager maintains, and an issue form that cannot be filled in
            without it. Its count is the number of rooms rather than the number of
            rows, because one row is a run and the manager is counting rooms. */}
        <SetGroup id="cat-rooms" level={1} title={t.adminRooms} sub={t.adminRoomsSub}
          count={roomTotal}>
          <div className="set-body">
            <div className="set-form">
              {roomErr && <div className="auth-error" style={{marginBottom:14}}>{roomErr}</div>}
              <form onSubmit={addRooms} className="admin-form">
                <div className="form-group">
                  <label htmlFor="set-rooms-add">{t.adminRoomsAdd}</label>
                  <input id="set-rooms-add" className="form-control" placeholder={t.adminRoomsPlaceholder}
                    value={roomRun} maxLength={40}
                    onChange={e=>{ setRoomRun(e.target.value); setRoomErr(''); }}/>
                </div>

                {/* The size of the range, before it is saved. Every whole number in
                    between is added, so this is the only thing standing between a
                    mistyped range and a dropdown full of rooms that do not exist. */}
                {roomPreview && (
                  <div style={{fontSize:12.5,marginTop:2,
                    color: roomPreview.count > maxRunSize ? 'var(--red)' : 'var(--gray-400)'}}>
                    {roomPreview.count > maxRunSize
                      ? t.adminRoomsTooWide
                          .replace('{count}', roomPreview.count)
                          .replace('{max}', maxRunSize)
                      : roomPreview.count === 1
                        ? t.adminRoomsOneRoom.replace('{from}', roomPreview.from)
                        : t.adminRoomsWillAdd
                            .replace('{count}', roomPreview.count)
                            .replace('{from}', roomPreview.from)
                            .replace('{to}', roomPreview.to)}
                  </div>
                )}

                <button type="submit" className="btn btn-primary" style={{marginTop:12}}
                  disabled={savingRoom || !roomRun.trim()
                    || !roomPreview || roomPreview.count > maxRunSize} aria-busy={savingRoom}>
                  {Ic.plus} {savingRoom ? '…' : t.adminRoomsAdd}
                </button>
              </form>
            </div>

            <div className="table-card">
              {rooms.length === 0
                ? <div className="empty-state">{Ic.list}<p>{t.adminRoomsEmpty}</p></div>
                : <table>
                    <thead>
                      <tr><th>{t.adminRoomsRun}</th><th style={{width:110}}></th></tr>
                    </thead>
                    <tbody>
                      {rooms.slice(0, roomsAll ? rooms.length : ROOMS_PREVIEW_ROWS).map(r=>(
                        <tr key={r.id}>
                          <td>
                            {/* The run as it was typed, and what it is worth in
                                rooms -- the second number is the one that says
                                whether the inventory covers a floor or a wing. */}
                            <div style={{display:'flex',alignItems:'baseline',gap:9}}>
                              <span style={{fontFamily:"'DM Mono',monospace",fontSize:13}}>
                                {r.from_room === r.to_room ? r.from_room : `${r.from_room}-${r.to_room}`}
                              </span>
                              {r.to_room !== r.from_room && (
                                <span style={{fontSize:12,color:'var(--gray-400)'}}>
                                  {t.roomsCount.replace('{count}', r.to_room - r.from_room + 1)}
                                </span>
                              )}
                            </div>
                          </td>
                          <td style={{textAlign:'right'}}>
                            <button className="icon-btn" title={t.adminRoomsDeleted}
                              onClick={()=>removeRooms(r.id)}>{Ic.trash}</button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
              }
              <AdminShowMore hidden={roomsAll ? 0 : Math.max(0, rooms.length - ROOMS_PREVIEW_ROWS)}
                label={t.adminShowMore} onClick={()=>setRoomsAll(true)}/>
            </div>
          </div>
        </SetGroup>

        {/* The shift templates. In Settings rather than on the rota page because
            they are configuration -- edited rarely, by somebody who knows the
            hotel's hours -- while the rota itself is edited often, by whoever is
            putting people on shifts. Two audiences, two pages.
            No count badge: the row is not a catalog of people, and a badge reading
            "3" against "Shifts" would be read as three shifts. */}
        <SetGroup id="cat-shifts" level={1} title={t.adminShifts} sub={t.adminShiftsSub}>
          <ShiftSettingsCard token={token} setToast={setToast}/>
        </SetGroup>

        {/* WHO is on the rota, beside WHAT the shifts are.

           Its own group rather than a panel inside cat-shifts, because they are
           different questions with different cadences: a shift is edited when the
           hotel's hours change, the team when somebody joins or leaves. And no count
           badge on the header, for the reason cat-shifts has none -- a bare number
           against "Team on the rota" reads as a number of shifts. The count lives
           inside the picker, next to the people it counts, where it can move while
           somebody is watching it. */}
        <SetGroup id="cat-members" level={1} title={t.rosterTitle} sub={t.rosterTitleSub}>
          <RosterCard token={token} setToast={setToast}/>
        </SetGroup>

      </SetGroup>

      {presetPickerOpen && (
        <ConsumablePicker items={consumableItems || []} value={presetConsumables}
          onCancel={()=>setPresetPickerOpen(false)}
          onDone={list=>{ setPresetConsumables(list); setPresetPickerOpen(false); }}/>
      )}
    </div>
  );
}

/* ════════════════ ROOT APP ════════════════ */
/* ── Error boundary ── */
/* Without this, any component that throws unmounts the whole tree and the user
   gets a blank white page with nothing in the console they can act on. Two real
   bugs hit exactly that: a nil `issues` array from the alerts API, and a merge
   conflict left in this file. A boundary turns "the app is gone" into "this one
   page broke, here is why, here is how to get back".

   It resets itself when the user navigates elsewhere, so a page that crashes
   once doesn't lock them out of every other page for the rest of the session. */
class PageErrorBoundary extends React.Component {
  static contextType = LangCtx;
  constructor(props) {
    super(props);
    this.state = { error: null, stack: '' };
  }
  static getDerivedStateFromError(error) { return { error }; }
  componentDidCatch(error, info) {
    // Kept in the console as well as the screen, since the component stack is
    // what actually identifies which of the pages is at fault.
    console.error('Page crashed:', error, info && info.componentStack);
    this.setState({ stack: (info && info.componentStack) || '' });
  }
  componentDidUpdate(prevProps) {
    if (this.state.error && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ error: null, stack: '' });
    }
  }
  render() {
    if (!this.state.error) return this.props.children;
    const t = T[this.context] || T.en;
    return (
      <div className="error-boundary">
        <div className="error-boundary-card">
          <div className="error-boundary-icon">{Ic.info}</div>
          <h2>{t.pageErrorTitle}</h2>
          <p className="error-boundary-msg">{t.pageErrorBody}</p>
          <pre className="error-boundary-detail">{String(this.state.error)}</pre>
          {this.state.stack && <pre className="error-boundary-stack">{this.state.stack.trim()}</pre>}
          <div className="error-boundary-actions">
            <button className="btn btn-primary" onClick={()=>this.setState({error:null, stack:''})}>{t.tryAgain}</button>
            <button className="btn btn-ghost" onClick={()=>location.reload()}>{t.reloadApp}</button>
          </div>
        </div>
      </div>
    );
  }
}


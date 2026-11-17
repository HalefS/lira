const ROOM_MAP_STATUSES = ['unassigned', 'unchecked', 'ok', 'degraded', 'down'];
const ROOM_MAP_STATUS_KEY = {
  unassigned: 'roomMapStatusUnassigned',
  unchecked:  'roomMapStatusUnchecked',
  ok:         'roomMapStatusOk',
  degraded:   'roomMapStatusDegraded',
  down:       'roomMapStatusDown',
};
const ROOM_MAP_STATUS_COLOR = {
  unassigned: 'var(--gray-400)',
  unchecked:  'var(--amber)',
  ok:         'var(--green)',
  degraded:   'var(--amber)',
  down:       'var(--red)',
};
const ROOM_MAP_ICON_OPTIONS = [
  ['box',    'roomMapIconBox'],
  ['tv',     'roomMapIconTv'],
  ['phone',  'roomMapIconPhone'],
  ['server', 'roomMapIconServer'],
  ['wifi',   'roomMapIconWifi'],
  ['lock',   'roomMapIconLock'],
];

const roomMapStatusLabel = (t, status) => t[ROOM_MAP_STATUS_KEY[status]] || status;

/* The catalog's icon token is data, so an unknown one must degrade to a plain
   box rather than draw nothing. */
function EquipmentGlyph({ icon }) {
  return <span className="rm-eq-icon">{Ic[icon] || Ic.box}</span>;
}

function RoomMapRoomDialog({ room, token, categories, equipmentTypes, t, setToast, onClose, onSaved }) {
  const [categoryId, setCategoryId] = React.useState(room.category_id ? String(room.category_id) : '');
  const [notes, setNotes] = React.useState(room.notes || '');
  const [counts, setCounts] = React.useState(() => {
    const init = {};
    for (const et of equipmentTypes) init[et.id] = { present: 0, working: 0 };
    for (const e of room.equipment || []) {
      init[e.equipment_type_id] = { present: e.present, working: e.working };
    }
    return init;
  });
  const [saving, setSaving] = React.useState(false);
  const [err, setErr] = React.useState('');
  // A room can be classified before anyone walks it. Saving a category alone
  // must leave the room "not checked", so "we have not looked" stays distinct
  // from "we looked and it is fine". The box starts ticked for a room that has
  // already been surveyed.
  const [checked, setChecked] = React.useState(!!room.surveyed_at);

  // The same keyboard contract as every other dialog in the app: Escape and
  // outside clicks close it, Tab is trapped inside, focus returns to the room
  // card afterwards, and neither escape hatch fires mid-save.
  const panelRef = useDialog({ onClose, preventClose: saving });

  const category = categories.find(c => String(c.id) === categoryId) || null;
  const expectedFor = id => {
    const line = ((category && category.equipment) || []).find(e => e.equipment_type_id === id);
    return line ? line.quantity : 0;
  };

  // What the category expects comes first, so a surveyor reads the list in the
  // order they check the room. Equipment outside the template follows, because
  // it can still be recorded (a router somebody added to one room).
  const orderedTypes = React.useMemo(
    () => [...equipmentTypes].sort((a, b) => {
      const ea = expectedFor(a.id) > 0 ? 0 : 1;
      const eb = expectedFor(b.id) > 0 ? 0 : 1;
      if (ea !== eb) return ea - eb;
      return a.name.localeCompare(b.name);
    }),
    [equipmentTypes, category]
  );

  // Present is the ceiling: raising working above it is the one thing a survey
  // must not be able to record, so the control clamps rather than validating
  // after the fact.
  function change(id, field, delta) {
    setCounts(prev => {
      const cur = { ...(prev[id] || { present: 0, working: 0 }) };
      if (field === 'present') {
        cur.present = Math.max(0, Math.min(99, cur.present + delta));
        if (cur.working > cur.present) cur.working = cur.present;
      } else {
        cur.working = Math.max(0, Math.min(cur.present, cur.working + delta));
      }
      return { ...prev, [id]: cur };
    });
  }

  async function save() {
    setSaving(true);
    setErr('');
    try {
      const equipment = equipmentTypes
        .map(et => ({
          equipment_type_id: et.id,
          present: (counts[et.id] && counts[et.id].present) || 0,
          working: (counts[et.id] && counts[et.id].working) || 0,
        }))
        // Zero rows carry no information: a type the category expects but
        // nobody counted is added back by the server as a zero, and a type the
        // category does not expect at zero says nothing at all.
        .filter(e => e.present > 0);

      const res = await apiFetch(`/room-map/${room.room_number}`, {
        method: 'PUT',
        token,
        body: {
          category_id: categoryId ? Number(categoryId) : null,
          notes,
          surveyed: checked,
          equipment,
        },
      });
      onSaved(res.room);
      setToast(t.roomMapSaved);
    } catch (ex) {
      setErr(ex.message || t.roomMapSaveError);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && !saving && onClose()}>
      <div className="modal" ref={panelRef} style={{ maxWidth: 620 }} role="dialog" aria-modal="true" aria-labelledby="rm-room-title">
        <div className="modal-header">
          <div>
            <h2 id="rm-room-title">{t.roomMapRoomTitle.replace('{room}', room.room_number)}</h2>
            <div style={{ fontSize: 11.5, color: 'var(--gray-400)', marginTop: 3 }}>
              {room.surveyed_at
                ? t.roomMapLastChecked.replace('{date}', new Date(room.surveyed_at).toLocaleString())
                : t.roomMapNeverChecked}
            </div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} disabled={saving} aria-label={t.roomMapClose}>{Ic.x}</button>
        </div>

        <div className="modal-body">
          <div style={{ display: 'flex', alignItems: 'center', gap: 7, fontSize: 12, color: ROOM_MAP_STATUS_COLOR[room.status] }}>
            <span className="rm-dot" style={{ background: ROOM_MAP_STATUS_COLOR[room.status] }} />
            <span style={{ fontWeight: 600 }}>{roomMapStatusLabel(t, room.status)}</span>
          </div>

          <div className="form-group">
            <label>{t.roomMapCategory}</label>
            <select className="form-control" value={categoryId} onChange={e => setCategoryId(e.target.value)}>
              <option value="">{t.roomMapNoCategory}</option>
              {categories.map(c => <option key={c.id} value={String(c.id)}>{c.name}</option>)}
            </select>
          </div>

          <label className="rm-check-line">
            <input type="checkbox" checked={checked} onChange={e => setChecked(e.target.checked)} />
            <span className="rm-check-text">
              <span className="rm-check-title">{t.roomMapSurveyed}</span>
              <span className="rm-check-hint">{t.roomMapSurveyedHint}</span>
            </span>
          </label>

          <div>
            <div className="form-group-h" style={{ marginBottom: 4 }}>{t.roomMapEquipment}</div>
            {equipmentTypes.length === 0 ? (
              <div style={{ fontSize: 12.5, color: 'var(--gray-400)' }}>{t.roomMapNoEquipment}</div>
            ) : orderedTypes.map(et => {
              const c = counts[et.id] || { present: 0, working: 0 };
              const expected = expectedFor(et.id);
              return (
                <div className="rm-eq-row" key={et.id}>
                  <div className="rm-eq-name">
                    <span className="rm-eq-title"><EquipmentGlyph icon={et.icon}/> {et.name}</span>
                    <span className="rm-eq-meta">
                      {expected > 0
                        ? t.roomMapExpected.replace('{count}', expected)
                        : t.roomMapNotExpected}
                    </span>
                  </div>
                  <div className="rm-eq-col">
                    <span className="rm-stepper-label">{t.roomMapPresent}</span>
                    <div className="rm-stepper">
                      <button type="button" onClick={() => change(et.id, 'present', -1)} disabled={c.present <= 0}
                        aria-label={t.roomMapFewerPresent.replace('{name}', et.name)}>{Ic.minus}</button>
                      <span>{c.present}</span>
                      <button type="button" onClick={() => change(et.id, 'present', 1)} disabled={c.present >= 99}
                        aria-label={t.roomMapMorePresent.replace('{name}', et.name)}>{Ic.plus}</button>
                    </div>
                  </div>
                  <div className="rm-eq-col">
                    <span className="rm-stepper-label">{t.roomMapWorking}</span>
                    <div className="rm-stepper">
                      <button type="button" onClick={() => change(et.id, 'working', -1)} disabled={c.working <= 0}
                        aria-label={t.roomMapFewerWorking.replace('{name}', et.name)}>{Ic.minus}</button>
                      <span>{c.working}</span>
                      <button type="button" onClick={() => change(et.id, 'working', 1)} disabled={c.working >= c.present}
                        aria-label={t.roomMapMoreWorking.replace('{name}', et.name)}>{Ic.plus}</button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>

          <div className="form-group">
            <label>{t.roomMapNotes}</label>
            <textarea className="form-control" rows={2} value={notes} onChange={e => setNotes(e.target.value)} />
          </div>

          {err && (
            <div style={{ background: 'var(--red-light)', color: 'var(--red)', borderRadius: 'var(--radius-md)', padding: '9px 12px', fontSize: 12.5 }}>
              {err}
            </div>
          )}
        </div>

        <div className="modal-footer">
          <button className="btn btn-ghost" onClick={onClose} disabled={saving}>{t.cancel}</button>
          <button className="btn btn-primary" onClick={save} disabled={saving}>
            {saving ? t.issueSaving : t.roomMapSave}
          </button>
        </div>
      </div>
    </div>
  );
}

function RoomMapCategoryManager({ token, categories, equipmentTypes, onReload, onClose, t, setToast }) {
  const [editId, setEditId] = React.useState(null);
  const [name, setName] = React.useState('');
  const [color, setColor] = React.useState('#1769AA');
  const [qty, setQty] = React.useState({});
  const [err, setErr] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [newName, setNewName] = React.useState('');
  const [newIcon, setNewIcon] = React.useState('box');
  const [etErr, setEtErr] = React.useState('');

  // Same keyboard contract as the room dialog and the rest of the app. A write
  // in flight cannot be escaped; nothing here is optimistic, so closing after
  // the response has landed is safe.
  const panelRef = useDialog({ onClose, preventClose: busy });

  function resetForm() {
    setEditId(null);
    setName('');
    setColor('#1769AA');
    setQty({});
    setErr('');
  }

  function startEdit(c) {
    setEditId(c.id);
    setName(c.name);
    setColor(c.color || '#1769AA');
    const next = {};
    for (const e of c.equipment || []) next[e.equipment_type_id] = String(e.quantity);
    setQty(next);
    setErr('');
  }

  async function saveCategory(e) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setErr('');
    try {
      const equipment = equipmentTypes
        .map(et => ({ equipment_type_id: et.id, quantity: Number(qty[et.id] || 0) }))
        .filter(e => e.quantity > 0);
      const body = { name, color, equipment };
      if (editId) await apiFetch(`/room-categories/${editId}`, { method: 'PATCH', token, body });
      else await apiFetch('/room-categories', { method: 'POST', token, body });
      await onReload();
      resetForm();
      setToast(t.roomMapCategorySaved);
    } catch (ex) {
      setErr(ex.message || t.roomMapManageError);
    } finally {
      setBusy(false);
    }
  }

  async function removeCategory(c) {
    if (!confirm(t.roomMapCategoryDeleteConfirm.replace('{name}', c.name))) return;
    setErr('');
    try {
      await apiFetch(`/room-categories/${c.id}`, { method: 'DELETE', token });
      if (editId === c.id) resetForm();
      await onReload();
      setToast(t.roomMapCategoryDeleted);
    } catch (ex) {
      setErr(ex.message || t.roomMapManageError);
    }
  }

  async function addEquipmentType(e) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setEtErr('');
    try {
      await apiFetch('/equipment-types', { method: 'POST', token, body: { name: newName, icon: newIcon } });
      setNewName('');
      await onReload();
      setToast(t.roomMapEquipmentSaved);
    } catch (ex) {
      setEtErr(ex.message || t.roomMapManageError);
    } finally {
      setBusy(false);
    }
  }

  async function removeEquipmentType(et) {
    if (!confirm(t.roomMapEquipmentDeleteConfirm.replace('{name}', et.name))) return;
    setEtErr('');
    try {
      await apiFetch(`/equipment-types/${et.id}`, { method: 'DELETE', token });
      await onReload();
      setToast(t.roomMapEquipmentDeleted);
    } catch (ex) {
      setEtErr(ex.message || t.roomMapManageError);
    }
  }

  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && !busy && onClose()}>
      <div className="modal" ref={panelRef} style={{ maxWidth: 720 }} role="dialog" aria-modal="true" aria-labelledby="rm-manager-title">
        <div className="modal-header">
          <div>
            <h2 id="rm-manager-title">{t.roomMapManagerTitle}</h2>
            <div style={{ fontSize: 11.5, color: 'var(--gray-400)', marginTop: 3, maxWidth: 520 }}>{t.roomMapManagerIntro}</div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} disabled={busy} aria-label={t.roomMapClose}>{Ic.x}</button>
        </div>

        <div className="modal-body">
          <div className="rm-cat-list">
            {categories.length === 0 ? (
              <div style={{ fontSize: 12.5, color: 'var(--gray-400)' }}>{t.roomMapNoCategories}</div>
            ) : categories.map(c => (
              <div className="rm-cat-row" key={c.id}>
                <span className="rm-cat-swatch" style={{ background: c.color || 'var(--gray-400)' }} />
                <div className="rm-cat-main">
                  <div className="rm-cat-name">{c.name}</div>
                  <div className="rm-cat-eq">
                    {(c.equipment || []).length === 0
                      ? t.roomMapNoEquipment
                      : c.equipment.map(e => `${e.quantity}× ${e.name}`).join(' · ')}
                  </div>
                </div>
                <div className="rm-cat-actions">
                  <button className="btn btn-ghost" style={{ padding: '3px 9px', fontSize: 12 }} onClick={() => startEdit(c)}>
                    {t.roomMapEdit}
                  </button>
                  <button className="btn btn-ghost" style={{ padding: '3px 9px', fontSize: 12, color: 'var(--red)' }} onClick={() => removeCategory(c)}>
                    {t.roomMapDelete}
                  </button>
                </div>
              </div>
            ))}
          </div>

          <form className="rm-cat-form" onSubmit={saveCategory}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              <div className="form-group-h">{editId ? t.roomMapEditCategory : t.roomMapNewCategory}</div>
              {editId && <button type="button" className="btn btn-ghost" style={{ padding: '3px 9px', fontSize: 12 }} onClick={resetForm}>{t.cancel}</button>}
            </div>

            <div className="form-row">
              <div className="form-group">
                <label>{t.roomMapCategoryName}</label>
                <input className="form-control" value={name} onChange={e => setName(e.target.value)} required />
              </div>
              <div className="form-group">
                <label>{t.roomMapColor}</label>
                <input type="color" className="form-control" style={{ padding: 3, height: 36 }} value={color} onChange={e => setColor(e.target.value)} />
              </div>
            </div>

            <div className="form-group">
              <label>{t.roomMapEquipmentTemplate}</label>
              <div className="rm-cat-eq-list">
                {equipmentTypes.map(et => (
                  <div className="rm-cat-eq-row" key={et.id}>
                    <span className="rm-cat-eq-name"><EquipmentGlyph icon={et.icon}/> {et.name}</span>
                    <input type="number" min="0" max="99" className="form-control" style={{ padding: '5px 8px', fontSize: 13 }}
                      value={qty[et.id] || ''} placeholder="0"
                      aria-label={`${t.roomMapQuantity}: ${et.name}`}
                      onChange={e => setQty(prev => ({ ...prev, [et.id]: e.target.value }))} />
                  </div>
                ))}
              </div>
            </div>

            {err && <div style={{ color: 'var(--red)', fontSize: 12.5 }}>{err}</div>}

            <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
              <button type="submit" className="btn btn-primary" disabled={busy} aria-busy={busy}>
                {busy ? t.issueSaving : t.roomMapSaveCategory}
              </button>
            </div>
          </form>

          <div>
            <div className="form-group-h" style={{ marginBottom: 7 }}>{t.roomMapEquipmentTypes}</div>
            <div className="rm-et-chips">
              {equipmentTypes.map(et => (
                <span className="rm-et-chip" key={et.id}>
                  <EquipmentGlyph icon={et.icon}/> {et.name}
                  <button type="button" onClick={() => removeEquipmentType(et)}
                    aria-label={`${t.roomMapDelete}: ${et.name}`}>{Ic.x}</button>
                </span>
              ))}
            </div>
          </div>

          <form className="rm-et-add" onSubmit={addEquipmentType}>
            <input className="form-control" style={{ flex: 1, minWidth: 150 }} placeholder={t.roomMapEquipmentName}
              value={newName} onChange={e => setNewName(e.target.value)} required />
            <select className="form-control" value={newIcon} onChange={e => setNewIcon(e.target.value)} aria-label={t.roomMapIcon}>
              {ROOM_MAP_ICON_OPTIONS.map(([value, key]) => <option key={value} value={value}>{t[key]}</option>)}
            </select>
            <button type="submit" className="btn btn-ghost" disabled={busy}>{Ic.plus} {t.roomMapAddEquipment}</button>
          </form>
          {etErr && <div style={{ color: 'var(--red)', fontSize: 12.5 }}>{etErr}</div>}
        </div>

        <div className="modal-footer">
          <button className="btn btn-primary" onClick={onClose} disabled={busy}>{t.roomMapClose}</button>
        </div>
      </div>
    </div>
  );
}

function RoomMapPage({ token, currentUser, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];
  const [data, setData] = React.useState(null);
  const [error, setError] = React.useState('');
  const [loading, setLoading] = React.useState(true);
  const [status, setStatus] = React.useState('all');
  const [floor, setFloor] = React.useState('all');
  const [query, setQuery] = React.useState('');
  const [openRoom, setOpenRoom] = React.useState(null);
  const [manage, setManage] = React.useState(false);
  const [retry, setRetry] = React.useState(0);

  async function load(silent) {
    if (!silent) setLoading(true);
    setError('');
    try {
      const res = await apiFetch('/room-map', { token });
      setData(res.room_map);
    } catch (ex) {
      setError(ex.message || t.roomMapLoadError);
    } finally {
      if (!silent) setLoading(false);
    }
  }

  React.useEffect(() => { load(false); }, [token, retry]);

  async function refresh() { await load(true); }

  const rooms = (data && data.rooms) || [];
  const categories = (data && data.categories) || [];
  const equipmentTypes = (data && data.equipment_types) || [];
  const summary = (data && data.summary) || { total: 0, unassigned: 0, unchecked: 0, ok: 0, degraded: 0, down: 0 };

  const floors = React.useMemo(
    () => [...new Set(rooms.map(r => r.floor))].sort((a, b) => a - b),
    [rooms]
  );

  const q = query.trim().toLowerCase();
  const shown = rooms.filter(r => {
    if (status !== 'all' && r.status !== status) return false;
    if (floor !== 'all' && r.floor !== floor) return false;
    if (q && !(`${r.room_number} ${r.category_name || ''}`.toLowerCase().includes(q))) return false;
    return true;
  });

  const byFloor = React.useMemo(() => {
    const out = {};
    for (const r of shown) (out[r.floor] = out[r.floor] || []).push(r);
    return Object.keys(out).sort((a, b) => Number(a) - Number(b)).map(f => [Number(f), out[f]]);
  }, [shown]);

  const tiles = [
    { id: 'all', label: t.roomMapAll, count: summary.total, color: 'var(--gray-900)' },
    ...ROOM_MAP_STATUSES.map(s => ({
      id: s, label: roomMapStatusLabel(t, s), count: summary[s] || 0, color: ROOM_MAP_STATUS_COLOR[s],
    })),
  ];

  return (
    <>
      <p style={{ fontSize: 13, color: 'var(--gray-400)', margin: '-6px 0 16px', maxWidth: 760 }}>{t.roomMapSubtitle}</p>

      <div className="rm-toolbar">
        <div className="search-wrap">
          {Ic.search}
          <input value={query} onChange={e => setQuery(e.target.value)} placeholder={t.roomMapSearch} aria-label={t.roomMapSearch}/>
        </div>
        <div className="filter-chips">
          <button className={`chip ${floor === 'all' ? 'active' : ''}`} aria-pressed={floor === 'all'} onClick={() => setFloor('all')}>{t.roomMapAllFloors}</button>
          {floors.map(f => (
            <button key={f} className={`chip ${floor === f ? 'active' : ''}`} aria-pressed={floor === f} onClick={() => setFloor(f)}>
              {t.roomMapFloorLabel.replace('{floor}', f)}
            </button>
          ))}
        </div>
        {currentUser && currentUser.role === 'manager' && (
          <button className="btn btn-ghost" onClick={() => setManage(true)}>{Ic.gear} {t.roomMapManage}</button>
        )}
      </div>

      {data && categories.length === 0 && (
        <div className="page-banner page-banner-info" style={{ marginBottom: 16 }}>
          {Ic.info}
          <span>{t.roomMapNoCategories}</span>
          {currentUser && currentUser.role === 'manager' && (
            <button className="page-banner-action" onClick={() => setManage(true)}>{t.roomMapManage}</button>
          )}
        </div>
      )}

      <div className="rm-summary">
        {tiles.map(tile => (
          <button key={tile.id} className={`rm-summary-card ${status === tile.id ? 'active' : ''}`}
            aria-pressed={status === tile.id} onClick={() => setStatus(tile.id)}>
            <span className="rm-summary-count" style={{ color: tile.color }}>{tile.count}</span>
            <span className="rm-summary-label">{tile.label}</span>
          </button>
        ))}
      </div>

      {loading ? (
        <div className="empty-state"><p>{t.roomMapLoading}</p></div>
      ) : error && !data ? (
        <div className="table-card">
          <div style={{ background: 'var(--red-light)', color: 'var(--red)', borderRadius: 'var(--radius-md)', padding: '10px 16px', fontSize: 13, margin: '14px 20px', display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ flex: 1 }}>{error}</span>
            <button className="btn btn-ghost" style={{ padding: '4px 12px', fontSize: 12 }} onClick={() => setRetry(n => n + 1)}>{t.tryAgain}</button>
          </div>
        </div>
      ) : rooms.length === 0 ? (
        <div className="empty-state">{Ic.map}<p>{t.roomMapNoRooms}</p></div>
      ) : shown.length === 0 ? (
        <div className="empty-state">{Ic.search}<p>{t.roomMapNoMatch}</p></div>
      ) : byFloor.map(([floorNumber, list]) => (
        <section className="rm-floor" key={floorNumber}>
          <div className="rm-floor-head">
            <span className="rm-floor-title">{t.roomMapFloorLabel.replace('{floor}', floorNumber)}</span>
            <span className="rm-floor-line" />
          </div>
          <div className="rm-grid">
            {list.map(r => (
              <button key={r.room_number} className={`rm-room rm-room-${r.status}`} onClick={() => setOpenRoom(r)}>
                <span className="rm-room-number">{r.room_number}</span>
                <span className="rm-room-cat">{r.category_name || t.roomMapNoCategory}</span>
                <span className="rm-room-foot">
                  <span className="rm-dot" style={{ background: ROOM_MAP_STATUS_COLOR[r.status] }} />
                  {roomMapStatusLabel(t, r.status)}
                </span>
              </button>
            ))}
          </div>
        </section>
      ))}

      {openRoom && (
        <RoomMapRoomDialog
          room={openRoom}
          token={token}
          categories={categories}
          equipmentTypes={equipmentTypes}
          t={t}
          setToast={setToast}
          onClose={() => setOpenRoom(null)}
          onSaved={async room => { setOpenRoom(null); await refresh(); }}
        />
      )}

      {manage && (
        <RoomMapCategoryManager
          token={token}
          categories={categories}
          equipmentTypes={equipmentTypes}
          onReload={refresh}
          onClose={() => setManage(false)}
          t={t}
          setToast={setToast}
        />
      )}
    </>
  );
}

/* Formats a current-vs-previous change: {pct, dir, color}. goodDirection
   is 'up', 'down', or 'neutral' - determines whether an increase should be
   shown as good (green) or bad (red). */

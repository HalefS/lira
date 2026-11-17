function ShiftSettingsCard({ token, setToast }) {
  const lang = React.useContext(LangCtx);
  const t = T[lang];

  const [shifts, setShifts] = useState(null);
  const [err, setErr] = useState('');
  const [saving, setSaving] = useState(false);
  const [editingId, setEditingId] = useState(null);
  const [draft, setDraft] = useState(null);   // {name, start_time, end_time}
  const [confirmDelete, setConfirmDelete] = useState(null);

  const [name, setName] = useState('');
  const [start, setStart] = useState('08:00');
  const [end, setEnd] = useState('16:00');
  // The tint of the shift being added. '' is no tint, which is what the server
  // stores as NULL and what an untinted shift renders as. Deliberately separate from
  // any existing shift's colour: this is a draft, and it is only sent on POST.
  const [newTint, setNewTint] = useState('');

  const load = useCallback(() => {
    return apiFetch('/shifts?include_retired=true', {token})
      .then(res => { setShifts(res.shifts || []); setErr(''); })
      .catch(ex => setErr((ex && ex.message) || 'Request failed'));
  }, [token]);

  useEffect(() => { load(); }, [load]);

  // Whether the times in the form cross midnight, so the form can say so before
  // the save rather than after it. Derived here with the same rule the server
  // uses -- the end is earlier in the day than the start, and is not midnight
  // itself -- because 16:00-00:00 is eight hours finishing at midnight and a bare
  // end < start gets that wrong.
  const looksOvernight = (() => {
    const mins = x => {
      const [h, m] = String(x || '').split(':').map(Number);
      return (h || 0) * 60 + (m || 0);
    };
    if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(start) || !/^([01]\d|2[0-3]):[0-5]\d$/.test(end)) return false;
    const e = mins(end);
    return e > 0 && e < mins(start);
  })();

  function beginAdd() {
    setName(''); setStart('08:00'); setEnd('16:00'); setErr('');
  }

  async function addShift(e) {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    try {
      await apiFetch('/shifts', {
        method: 'POST', token,
        body: { name: name.trim(), start_time: start, end_time: end, color: newTint || null },
      });
      setName(''); setNewTint(''); setErr('');
      await load();
      setToast(t.shiftAdded);
    } catch (ex) {
      setErr((ex && ex.message) || 'Request failed');
    } finally {
      setSaving(false);
    }
  }

  function beginEdit(s) {
    setEditingId(s.id);
    setDraft({ name: s.name, start_time: s.start_time, end_time: s.end_time });
    setErr('');
  }

  async function saveEdit(id, version) {
    if (!draft || !draft.name.trim()) return;
    setSaving(true);
    try {
      await apiFetch('/shifts/' + id, {
        method: 'PATCH', token,
        body: {
          name: draft.name.trim(),
          start_time: draft.start_time,
          end_time: draft.end_time,
          // No colour, and not by oversight. The inline editor shows a dot rather
          // than a control and the draft holds no tint, because the tint saves the
          // moment it is picked. The server defaults a missing tint from the stored
          // row, so a rename cannot wipe one.
          active: true,
          version,
        },
      });
      setEditingId(null); setDraft(null); setErr('');
      await load();
      setToast(t.shiftUpdated);
    } catch (ex) {
      // A 409 is the server saying somebody else saved first. The message it
      // sends already says so, and the reload is what makes the row editable
      // again -- the version the form holds is no longer current.
      setErr((ex && ex.message) || 'Request failed');
      if (ex && ex.status === 409) { setEditingId(null); setDraft(null); load(); }
    } finally {
      setSaving(false);
    }
  }

  /* Changing one shift's tint.

     A single PATCH carrying the WHOLE ROW, not a patch of the colour. The server
     writes the columns it is given and defaults a missing tint from the stored one,
     so omitting it would work -- but three writes on this card that each assemble a
     slightly different body is how a colour change silently reverts somebody else's
     rename. All three send name, times, colour, active and version, which is also
     the shape the server's validation expects. */
  async function setTint(s, color) {
    setSaving(true);
    try {
      await apiFetch('/shifts/' + s.id, {
        method: 'PATCH', token,
        body: { name: s.name, start_time: s.start_time, end_time: s.end_time,
                color: color || null, active: s.active, version: s.version },
      });
      await load();
      setToast(color ? t.shiftTintApplied : t.shiftTintReverted);
    } catch (ex) {
      setErr((ex && ex.message) || 'Request failed');
    } finally {
      setSaving(false);
    }
  }

  async function setActive(s, active) {
    setSaving(true);
    try {
      await apiFetch('/shifts/' + s.id, {
        method: 'PATCH', token,
        body: { name: s.name, start_time: s.start_time, end_time: s.end_time,
                color: s.color || null, active, version: s.version },
      });
      await load();
    } catch (ex) {
      setErr((ex && ex.message) || 'Request failed');
    } finally {
      setSaving(false);
    }
  }

  async function doDelete(s) {
    setSaving(true);
    try {
      await apiFetch('/shifts/' + s.id, { method: 'DELETE', token });
      setConfirmDelete(null);
      await load();
      setToast(t.shiftDeleted);
    } catch (ex) {
      setErr((ex && ex.message) || 'Request failed');
      setConfirmDelete(null);
    } finally {
      setSaving(false);
    }
  }

  if (!shifts) {
    return <div className="set-form">{Ic.calendar}<p style={{margin:0}}>{t.analyticsLoading}</p></div>;
  }

  return (
    <div className="set-body">
      <div className="set-form">
        <div className="set-form-cap">{t.shiftAdd}</div>
        {err && <div className="auth-error" style={{marginBottom:14}}>{err}</div>}
        <form onSubmit={addShift}>
          <div className="form-group">
            <label htmlFor="shift-new-name">{t.shiftName}</label>
            <input id="shift-new-name" className="form-control" maxLength={50}
              placeholder={t.shiftNamePlaceholder} value={name}
              onChange={e => { setName(e.target.value); setErr(''); }}/>
          </div>
          <div className="form-group" style={{marginTop:14}}>
            {/* A caption over a group of two fields, not a label for one of them. */}
            <div className="form-group-h" id="shift-new-times-h">{t.shiftTimes}</div>
            <div className="rota-settings-times" role="group" aria-labelledby="shift-new-times-h">
              <label htmlFor="shift-new-start" className="sr-only">{t.shiftStart}</label>
              <input id="shift-new-start" className="form-control" type="time" step={60}
                value={start} onChange={e => { setStart(e.target.value); setErr(''); }}/>
              <span className="rota-settings-dash">&ndash;</span>
              <label htmlFor="shift-new-end" className="sr-only">{t.shiftEnd}</label>
              <input id="shift-new-end" className="form-control" type="time" step={60}
                value={end} onChange={e => { setEnd(e.target.value); setErr(''); }}/>
            </div>
          </div>
          {looksOvernight && (
            <div className="rota-settings-night">
              {Ic.clock}<span>{t.shiftOvernightHint}</span>
            </div>
          )}
          <div className="form-group" style={{marginTop:14}}>
            {/* A caption over a control, not a label for one field -- the same shape
                as the times group above. */}
            <div className="form-group-h" id="shift-new-tint-h">{t.shiftTint}</div>
            <ShiftTintButton t={t} shift={{ id: 'new', name: t.shiftNewUntitled, color: newTint }}
              onPick={c => { setNewTint(c); setErr(''); }}/>
            {/* Said at the point of choosing, not in a policy page: this is the cue
                that says what the colour is and is not for, at the moment somebody
                decides whether to bother. */}
            <div className="rota-settings-tint-hint">{t.shiftTintHint}</div>
          </div>
          <div className="admin-form-inline" style={{marginTop:16}}>
            <span className="ci-preview">
              <span style={{fontFamily:"'DM Mono',monospace",fontSize:13}}>
                {start}&ndash;{end}{looksOvernight ? t.rotaPlusOne : ''}
              </span>
            </span>
            <button type="submit" className="btn btn-primary" disabled={saving || !name.trim()} aria-busy={saving}>
              {Ic.plus} {saving ? '.' : t.shiftAdd}
            </button>
          </div>
        </form>
      </div>

      <div className="table-card">
        {shifts.length === 0
          ? (
            <div className="empty-state">
              {Ic.clock}
              <p className="rota-empty-head">{t.shiftNoneYet}</p>
              <p className="rota-empty-sub">{t.shiftNoneYetBody}</p>
            </div>
          )
          : (
            <table>
              <thead>
                <tr>
                  <th>{t.shiftName}</th>
                  <th>{t.shiftTimes}</th>
                  <th>{t.shiftInUse}</th>
                  <th style={{width:120}}></th>
                </tr>
              </thead>
              <tbody>
                {shifts.map(s => (
                  <tr key={s.id}>
                    {editingId === s.id
                      ? (
                        <td>
                          <input className="form-control" autoFocus maxLength={50}
                            aria-label={t.shiftEdit + ' - ' + s.name}
                            value={draft.name}
                            onChange={e => setDraft({ ...draft, name: e.target.value })}
                            onKeyDown={e => {
                              if (e.key === 'Enter') saveEdit(s.id, s.version);
                              if (e.key === 'Escape') { setEditingId(null); setDraft(null); }
                            }}/>
                        </td>
                      )
                      : (
                        <td>
                          <div style={{display:'flex',alignItems:'center',gap:8,minWidth:0}}>
                            <span style={{fontWeight:500,overflow:'hidden',textOverflow:'ellipsis',whiteSpace:'nowrap'}}>
                              {s.name}
                            </span>
                            {s.active === false && <span className="rota-legend-retired">{t.rotaRetired}</span>}
                          </div>
                        </td>
                      )}
                    <td>
                      {editingId === s.id
                        ? (
                          <div className="rota-settings-hours">
                            {/* A dot, not a control. The row is already three inputs
                                and two buttons, and a fourth control makes the actions
                                cell four and pushes Save off its alignment. And the
                                tint saves immediately while the name and times save on
                                Save -- mixing an immediately-committing control into a
                                transaction row is how somebody changes a colour, presses
                                Cancel, and does not know which of the two happened. A
                                dot is honest: it shows the SAVED colour and offers
                                nothing. */}
                            <span className="rota-tint-dot" style={dotStyle(s.color)}
                              title={shiftTintName(t, s.color || '')}/>
                            <div className="rota-settings-times">
                            <input className="form-control" type="time" step={60}
                              aria-label={t.shiftStart + ' - ' + s.name}
                              value={draft.start_time}
                              onChange={e => setDraft({ ...draft, start_time: e.target.value })}/>
                            <span className="rota-settings-dash">&ndash;</span>
                            <input className="form-control" type="time" step={60}
                              aria-label={t.shiftEnd + ' - ' + s.name}
                              value={draft.end_time}
                              onChange={e => setDraft({ ...draft, end_time: e.target.value })}/>
                            </div>
                          </div>
                        )
                        : (
                          /* The tint and the night marker both ride in the Hours cell,
                             not in a column of their own and not in the actions cell.
                             It is the only cell that already carries a visual property,
                             it is where the reading happens anyway, and it means a
                             reader who has learned "the hours cell is where this
                             shift's marks live" has one place to look. It also keeps
                             the tint in the SAME COLUMN when Edit is pressed, which a
                             mark that jumped columns would not. */
                          <div className="rota-settings-hours">
                            <ShiftTintButton t={t} shift={s} onPick={c => setTint(s, c)}/>
                            <span className={'rota-cell-name' + (s.overnight ? ' is-night' : '')}
                              style={{fontFamily:"'DM Mono',monospace",fontSize:12.5,
                                      borderLeft:s.overnight ? '3px solid var(--orange)' : undefined,
                                      paddingLeft:s.overnight ? 6 : undefined,
                                      display:'inline-block'}}>
                              {s.start_time}&ndash;{s.end_time}{s.overnight ? t.rotaPlusOne : ''}
                            </span>
                          </div>
                        )}
                    </td>
                    <td style={{fontSize:12,color:'var(--gray-400)'}}>
                      {s.assignment_count > 0
                        ? (s.assignment_count === 1
                          ? t.shiftInUseOne
                          : t.shiftInUseMany.replace('{count}', s.assignment_count))
                        : ''}
                    </td>
                    <td style={{textAlign:'right',whiteSpace:'nowrap'}}>
                      {editingId === s.id
                        ? (
                          <>
                            <button className="btn btn-primary" style={{padding:'4px 12px',fontSize:12}}
                              onClick={() => saveEdit(s.id, s.version)}
                              disabled={saving || !draft.name.trim()} aria-busy={saving}>
                              {saving ? '.' : t.shiftSaveChanges}
                            </button>
                            <button className="btn btn-ghost" style={{padding:'4px 12px',fontSize:12}}
                              onClick={() => { setEditingId(null); setDraft(null); }}
                              disabled={saving} aria-busy={saving}>
                              {t.shiftCancelEdit}
                            </button>
                          </>
                        )
                        : (
                          <>
                            <button className="icon-btn" title={t.shiftEdit} onClick={() => beginEdit(s)}>
                              {Ic.edit}
                            </button>
                            {/* Retiring rather than deleting is the ordinary act here:
                                a shift is removed once nothing is on it, and deleting
                                one that is still in use empties those days silently. */}
                            <button className="icon-btn"
                              title={s.active === false ? t.shiftReinstate : t.shiftRetire}
                              onClick={() => setActive(s, s.active === false)}>
                              {s.active === false ? Ic.refresh : Ic.eyeoff}
                            </button>
                            <button className="icon-btn" title={t.removeType}
                              onClick={() => setConfirmDelete(s)}>
                              {Ic.trash}
                            </button>
                          </>
                        )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        {/* Stated rather than implied: deleting a shift empties the days that
            were on it, and the count is the only way a manager finds that out
            before pressing the button. */}
        {confirmDelete && (
          <div className="rota-savebar">
            {Ic.info}
            <span>
              {confirmDelete.assignment_count === 1
                ? t.shiftDeleteConfirmOne.replace('{count}', confirmDelete.assignment_count)
                : t.shiftDeleteConfirmMany.replace('{count}', confirmDelete.assignment_count)}
            </span>
            <span className="rota-savebar-spacer" />
            <button type="button" className="btn btn-ghost" onClick={() => setConfirmDelete(null)}>
              {t.cancel}
            </button>
            <button type="button" className="btn btn-primary" onClick={() => doDelete(confirmDelete)}>
              {Ic.trash} {saving ? '.' : t.shiftDeleteConfirmYes}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

/* Marking somebody away.

   Follows DiscardDialog exactly: useDialog with the initial focus on the SAFE action,
   modal-overlay > modal > role="dialog" aria-modal, a header with an h2 and an X, a
   body, and a footer. Cancel takes the focus rather than Save, for the reason
   ConfirmDeleteIssue and DiscardDialog both give -- Enter is often still held over
   from whatever the manager did before the dialog opened, and here a reflexive Enter
   would write a dated absence onto a colleague's record.

   Delete is NOT in this dialog's primary position. A footer carrying both Save and
   Remove is a mis-click generator, and this file already separates the two acts
   elsewhere: the Settings card puts its delete confirmation under the list, and
   ConfirmDeleteIssue is its own component. So Remove opens a second dialog with the
   same shape.

   The NOTE carries its privacy rule with it, at the field, rather than in a policy
   page: somebody typing a diagnosis deserves to know at the moment of typing that it
   will not reach a noticeboard. The server enforces that -- AbsenceRef, the shape the
   public grid carries, has no note field at all -- so this text is reassurance rather
   than the mechanism. */

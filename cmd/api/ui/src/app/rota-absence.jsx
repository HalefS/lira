function AbsenceDialog({ t, dlg, setDlg, spanDays, err, busy, onCancel, onSave, onRemove }) {
  const panelRef = useDialog({ onClose: busy ? null : onCancel, initialFocus: '#rota-abs-cancel' });
  const invalid = !dlg.starts_on || !dlg.ends_on || dlg.ends_on < dlg.starts_on;

  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && !busy && onCancel()}>
      <div className="modal confirm-modal" ref={panelRef} role="dialog" aria-modal="true"
        aria-labelledby="rota-abs-title">
        <div className="modal-header">
          <h2 id="rota-abs-title">{dlg.id ? t.rotaAbsEditTitle : t.rotaAbsAddTitle}</h2>
          <button type="button" className="icon-btn" onClick={onCancel} disabled={busy}>{Ic.x}</button>
        </div>

        <div className="modal-body">
          {/* Who this is about, stated in the dialog rather than left to the cell the
              manager clicked from -- ConfirmDeleteIssue repeats its subject line for
              the same reason: the confirm can be triggered from a row they are
              looking at rather than at. */}
          <div className="rota-abs-who">{Ic.person}<span>{dlg.name}</span></div>

          {/* A fieldset with a legend, not a div with a caption. Four radios are one
              question and the fieldset is the element that says so. */}
          <fieldset className="rota-abs-kinds">
            <legend className="form-group-h">{t.rotaAbsKindLabel}</legend>
            <div className="rota-abs-kindgrid">
              {ABSENCE_KINDS.map(k => (
                <label key={k.id}
                  className={'rota-abs-kind' + (dlg.kind === k.id ? ' is-on' : '')}
                  style={{ '--abs-ink': k.hue }}>
                  <input type="radio" name="rota-abs-kind" value={k.id}
                    checked={dlg.kind === k.id}
                    onChange={() => setDlg({ ...dlg, kind: k.id })}/>
                  <span className="rota-abs-kind-ic" aria-hidden="true">{absenceGlyph[k.id]}</span>
                  <span className="rota-abs-kind-name">{t.rotaAbsKind[k.id]}</span>
                </label>
              ))}
            </div>
          </fieldset>

          <div className="form-row">
            <div className="form-group">
              <label htmlFor="rota-abs-from">{t.rotaAbsFrom}</label>
              <input id="rota-abs-from" type="date" className="form-control"
                value={dlg.starts_on}
                onChange={e => setDlg({ ...dlg, starts_on: e.target.value })}/>
            </div>
            <div className="form-group">
              <label htmlFor="rota-abs-to">{t.rotaAbsTo}</label>
              <input id="rota-abs-to" type="date" className="form-control"
                value={dlg.ends_on} min={dlg.starts_on}
                onChange={e => setDlg({ ...dlg, ends_on: e.target.value })}/>
            </div>
          </div>

          {/* Stated rather than predicted. A run longer than the week on screen is the
              NORMAL case for this feature, not an edge case: a manager typing
              Monday-to-Friday while looking at Monday-to-Sunday needs to be told how
              many days they have just booked. Computed by absenceDayCount, which is
              UTC-based for the DST reason its own comment gives. */}
          <div className="rota-abs-span">
            {spanDays === 1 ? t.rotaAbsSpanOne : t.rotaAbsSpan.replace('{count}', spanDays)}
          </div>

          <div className="form-group">
            <label htmlFor="rota-abs-note">{t.rotaAbsNote}</label>
            <textarea id="rota-abs-note" className="form-control rota-abs-note" rows={3}
              maxLength={200} value={dlg.note || ''}
              placeholder={t.rotaAbsNotePlaceholder}
              onChange={e => setDlg({ ...dlg, note: e.target.value })}/>
            <div className="rota-abs-privacy">
              {Ic.shield}<span>{t.rotaAbsNotePrivacy}</span>
            </div>
          </div>

          {err && <div className="auth-error">{err}</div>}
        </div>

        <div className="modal-footer rota-abs-footer">
          <span className="rota-abs-left">
            {dlg.id
              ? <button type="button" className="btn btn-danger" onClick={onRemove} disabled={busy}>
                  {Ic.trash} {t.rotaAbsRemove}
                </button>
              : null}
          </span>
          <button type="button" id="rota-abs-cancel" className="btn btn-ghost"
            onClick={onCancel} disabled={busy} autoFocus>{t.rotaCancel}</button>
          <button type="button" className="btn btn-primary" onClick={onSave}
            disabled={busy || invalid}>
            {Ic.check} {busy ? '.' : (dlg.id ? t.rotaAbsSaveChanges : t.rotaAbsSave)}
          </button>
        </div>
      </div>
    </div>
  );
}

/* Removing one.

   The body copy is the load-bearing part: it says the recurring rota comes BACK, not
   that the days are removed. The recurring shift was never touched, and a manager who
   believes otherwise will delete a holiday and lose four Earlys. The server says the
   same thing in its DELETE response, for the same reader. */
function ConfirmRemoveAbsence({ t, target, onCancel, onConfirm, busy }) {
  const panelRef = useDialog({ onClose: busy ? null : onCancel, initialFocus: '#rota-abs-del-cancel' });
  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && !busy && onCancel()}>
      <div className="modal confirm-modal" ref={panelRef} role="dialog" aria-modal="true"
        aria-label={t.rotaAbsRemoveTitle}>
        <div className="modal-header">
          <h2>{t.rotaAbsRemoveTitle}</h2>
          <button type="button" className="icon-btn" onClick={onCancel} disabled={busy}>{Ic.x}</button>
        </div>
        <div className="modal-body confirm-body">
          <span className="confirm-icon">{Ic.trash}</span>
          <div className="confirm-text">
            {t.rotaAbsRemoveBody.replace('{name}', target.name)}
            <span className="confirm-subject">{target.starts_on} &ndash; {target.ends_on}</span>
          </div>
        </div>
        <div className="modal-footer">
          <button type="button" id="rota-abs-del-cancel" className="btn btn-ghost"
            onClick={onCancel} disabled={busy} autoFocus>{t.rotaCancel}</button>
          <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={busy}>
            {busy ? t.deleting : <>{Ic.trash} {t.rotaAbsRemoveYes}</>}
          </button>
        </div>
      </div>
    </div>
  );
}

/* Throwing away unsaved edits.

   Every other confirmation in this app is destructive and uses btn-danger; this
   one is too -- it discards work -- so it says so in the same colour as the delete
   button, and Cancel takes the initial focus, because a reflexive Enter after
   mis-clicking Cancel must not throw the edits away. useDialog is what gives it
   Escape and the Tab trap; a hand-rolled role=dialog has neither, whatever its
   aria-modal attribute says. */
function DiscardDialog({ t, count, onCancel, onDiscard }) {
  const panelRef = useDialog({ onClose: onCancel, initialFocus: '#rota-discard-cancel' });
  const body = count === 1
    ? t.rotaDiscardBody
    : t.rotaDiscardBodyMany.replace('{count}', count);

  return (
    <div className="modal-overlay" onClick={e => e.target === e.currentTarget && onCancel()}>
      <div className="modal confirm-modal" ref={panelRef} role="dialog" aria-modal="true"
        aria-labelledby="rota-discard-title">
        <div className="modal-header">
          <h2 id="rota-discard-title">{t.rotaDiscardTitle}</h2>
          <button type="button" className="icon-btn" onClick={onCancel}>{Ic.x}</button>
        </div>
        <div className="modal-body confirm-body">
          <span className="confirm-icon">{Ic.info}</span>
          <div className="confirm-text">{body}</div>
        </div>
        <div className="modal-footer">
          <button type="button" id="rota-discard-cancel" className="btn btn-ghost"
            onClick={onCancel} autoFocus>
            {t.rotaCancel}
          </button>
          <button type="button" className="btn btn-danger" onClick={onDiscard}>
            {t.yesDiscard}
          </button>
        </div>
      </div>
    </div>
  );
}

/* The rota page.

   PUBLIC. /v1/schedule takes no token, and apiFetch omits the Authorization header
   when there is none -- which is exactly the anonymous case the server answers
   with can_edit:false. So `token` is undefined here for a visitor who has not
   signed in, and that is not an error path; it is the common one.

   Two modes, and the boundary between them is the load-bearing decision on this
   page. The write is a PUT carrying all seven days and it CLEARS anything
   omitted, so an always-editable grid would build a seven-element payload out of
   current state on every cell interaction, and a bug in that construction does not
   show as a wrong cell -- it shows as somebody who silently lost four shifts, and
   a standing rota has no per-date diff for that to be noticed in. Building the
   payload once, from a frozen draft, on the way out of edit mode is where that
   class of bug stops existing. */

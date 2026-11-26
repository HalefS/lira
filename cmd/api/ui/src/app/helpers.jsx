/* ── helpers ── */
function today(lang) {
  return new Date().toLocaleDateString(lang==='pt' ? 'pt-PT' : 'en-GB', { weekday:'long', day:'numeric', month:'long', year:'numeric' });
}
/* Format a Date as YYYY-MM-DD in the *local* calendar.
   Date.prototype.toISOString() is UTC, so for a Date built at local midnight it
   returns the previous day anywhere east of Greenwich — at Europe/Lisbon in
   summer (+01:00) '2026-10-05T00:00:00' formats as '2026-10-04'. That silently
   moved the week selector: clicking "next week" on a Monday sent the previous
   Sunday, the server resolved the same week, and the button appeared dead. */
function localISODate(d) {
  return d.getFullYear() + '-' + String(d.getMonth()+1).padStart(2,'0') + '-' + String(d.getDate()).padStart(2,'0');
}
// Hoisted from SupportRequestModal so the issue form can use it too. One
// implementation, two callers, and the alternative -- a second copy -- is how
// the two formats drift.
function localDateTimeValue(d) {
  const pad = n => String(n).padStart(2,'0');
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
function initials(name) {
  return name.trim().split(' ').map(p=>p[0]).join('').slice(0,2).toUpperCase();
}
function loadLS(key, fallback) {
  try { const v = localStorage.getItem(key); return v ? JSON.parse(v) : fallback; } catch { return fallback; }
}
function saveLS(key, val) { try { localStorage.setItem(key, JSON.stringify(val)); } catch {} }

/* A stable identity for this browser profile, used when signing in.
   The server revokes only the token it issued to the same device, so a phone in
   a plant room and the reception desk can hold sessions for one account at the
   same time. Without an id the server falls back to revoking every token, which
   is what a new sign-in used to do -- correct for a shared machine, but wrong
   for the second device this app now runs on.

   The id is generated once and kept out of the session object, so it survives
   sign-out: signing in again on the same phone should replace this phone's
   token, not create a second one. */
function deviceIdentity() {
  let id = loadLS('lira_device', null);
  if (!id) {
    id = newUUID();
    saveLS('lira_device', id);
  }
  const ua = (typeof navigator !== 'undefined' && navigator.userAgent) || '';
  let name = 'Web';
  if (/iPhone/i.test(ua)) name = 'iPhone';
  else if (/iPad/i.test(ua)) name = 'iPad';
  else if (/Android/i.test(ua)) name = 'Android';
  else if (/Windows/i.test(ua)) name = 'Windows';
  else if (/Macintosh|Mac OS X/i.test(ua)) name = 'Mac';
  else if (/Linux/i.test(ua)) name = 'Linux';
  return { id: String(id).slice(0, 100), name: name.slice(0, 100) };
}

/* ── Map API issue → frontend shape ── */
function mapIssue(i) {
  return {
    id:            i.id,
    apt:           i.mode === 'apt'  ? i.location : undefined,
    dept:          i.mode === 'dept' ? i.location : undefined,
    mode:          i.mode,
    type:          i.type,
    problem:       i.problem,
    resolution:    i.resolution,
    time:          i.time_minutes,
    startTime:     i.start_time || undefined,
    endTime:       i.end_time || undefined,
    // Only ever read/written by the add/edit issue modal — never shown in
    // any table, history, or dashboard view.
    reportedByAgent:  i.reported_by_agent || undefined,
    confirmedByAgent: i.confirmed_by_agent || undefined,
    falsePositive: !!i.false_positive,
    // Consumables used on the issue: [{itemId, item, icon, quantity}]. itemId is
    // null when the item has since been removed from the catalog.
    consumables: (i.consumables || []).map(c => ({
      itemId: c.item_id ?? null, item: c.item, icon: c.icon, quantity: c.quantity,
    })),
    // Handovers to Telnet / Telefonica. The Telnet names are free text — they
    // are whoever we spoke to on the other side, not users of this app.
    // TVs moved between rooms to fix this issue. Rooms are free text, because
    // there is no rooms table in this system.
    tvSwaps: (i.tv_swaps || []).map(s => ({
      id: s.id, fromRoom: s.from_room, toRoom: s.to_room, notes: s.notes || '',
    })),
    supportRequests: (i.support_requests || []).map(s => ({
      id:            s.id,
      company:       s.company,
      // The catalog's current name and colour, joined by the server. Copied
      // onto the handover so a table can render it without loading the whole
      // catalog on every page.
      companyName:   s.company_name || s.company,
      companyColor:  s.company_color || 'gray',
      status:        s.status,
      technician:    s.telnet_technician || '',
      confirmedBy:   s.telnet_confirmed_by || '',
      ticketId:      s.telefonica_ticket_id || '',
      reference:     s.reference || '',
      // The company's turnaround, kept separate from the issue's own time.
      // Sliced to "YYYY-MM-DDTHH:MM" for the datetime-local inputs, which is
      // the format the API takes them back in.
      startedAt:     s.started_at  ? s.started_at.slice(0,16)  : '',
      resolvedAt:    s.resolved_at ? s.resolved_at.slice(0,16) : '',
      durationMinutes: s.duration_minutes,
      notes:         s.notes || '',
    })),
    status:        i.status,
    loggedBy:      i.logged_by,
    loggedByName:  i.logged_by_name,
    loggedByIdx:   i.logged_by_idx,
    createdAt:     i.created_at?.slice(0,10),
    version:       i.version,
    // Photos on the issue, oldest first. Metadata only -- the bytes are fetched
    // per photo by PhotoImage, never carried in the list JSON.
    attachments: (i.attachments || []).map(p => ({
      id: p.id, mimeType: p.mime_type, byteSize: p.byte_size,
      width: p.width, height: p.height, createdAt: p.created_at,
      createdByName: p.created_by_name || '',
    })),
  };
}


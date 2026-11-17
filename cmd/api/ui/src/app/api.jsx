/* ── API config ── */
const API = '/v1';

/* -- Offline storage -------------------------------------------------------

   Two things outlive a page load on a phone: the last answer the server gave,
   and work the phone could not send yet. Both belong in IndexedDB rather than
   localStorage: a page of issues is a few hundred kilobytes, and localStorage
   is synchronous, small, and already holds the session token.

   Reads are namespaced by account, not by device. Two people share the phone at
   the front desk, and the second must not open the app to the first one's
   issues because the first forgot to sign out. The outbox is namespaced the
   same way and only ever drains for the account that queued it.

   Every operation here is best-effort. Private browsing, a full disk or a
   blocked database must leave the online app exactly as it was. */

const OFFLINE_DB = 'lira-offline';
const OFFLINE_DB_VERSION = 2;
// One cached read larger than this is not worth the quota: it is almost
// certainly a report payload that can be fetched again, and keeping it would
// push out the pages the phone opens every day.
const READ_CACHE_MAX_CHARS = 512 * 1024;

let offlineDBPromise = null;

function offlineDB() {
  if (typeof indexedDB === 'undefined') return Promise.resolve(null);
  if (offlineDBPromise) return offlineDBPromise;
  offlineDBPromise = new Promise(resolve => {
    let req;
    try {
      req = indexedDB.open(OFFLINE_DB, OFFLINE_DB_VERSION);
    } catch (_) {
      resolve(null);
      return;
    }
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains('reads')) {
        const reads = db.createObjectStore('reads', { keyPath: 'key' });
        reads.createIndex('by_user', 'user');
      }
      // Created ahead of the queue that uses it, so a phone that received this
      // build does not need a second schema upgrade to start capturing work.
      if (!db.objectStoreNames.contains('outbox')) {
        const outbox = db.createObjectStore('outbox', { keyPath: 'id' });
        outbox.createIndex('by_user', 'user');
      } else {
        // Version 1 created the store without this index; adding it here keeps
        // existing installs on the same schema instead of making them wait for a
        // second upgrade.
        const outbox = req.transaction.objectStore('outbox');
        if (!outbox.indexNames.contains('by_user')) outbox.createIndex('by_user', 'user');
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => resolve(null);
    req.onblocked = () => resolve(null);
  });
  return offlineDBPromise;
}

function idbResult(req) {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function idbDone(tx) {
  return new Promise(resolve => {
    tx.oncomplete = () => resolve(true);
    tx.onerror = () => resolve(false);
    tx.onabort = () => resolve(false);
  });
}

// Which account the read cache belongs to. Set from the session, cleared on
// sign-out. While null, reads are neither stored nor served from the cache.
let readCacheUser = null;
// null = nothing read yet, true/false = the source of the last GET. Reset when
// the account changes so the next read reports itself again.
let lastReadWasCached = null;

function setReadCacheUser(user) {
  const id = user && user.id != null ? String(user.id) : null;
  if (id !== readCacheUser) {
    readCacheUser = id;
    lastReadWasCached = null;
  }
}

function readCacheKey(path) {
  return readCacheUser ? readCacheUser + '::' + path : null;
}

async function readCacheGet(path) {
  const key = readCacheKey(path);
  if (!key) return null;
  try {
    const db = await offlineDB();
    if (!db) return null;
    const row = await idbResult(db.transaction('reads', 'readonly').objectStore('reads').get(key));
    return row || null;
  } catch (_) {
    return null;
  }
}

function readCachePut(path, data) {
  const key = readCacheKey(path);
  if (!key) return;
  // Captured with the key: if the account changes before the queued write runs,
  // the row must still carry the account that made the request, or the next
  // sign-out would delete the wrong user's cache.
  const user = readCacheUser;
  let encoded;
  try { encoded = JSON.stringify(data); } catch (_) { return; }
  if (!encoded || encoded.length > READ_CACHE_MAX_CHARS) return;
  offlineDB().then(db => {
    if (!db) return;
    try {
      const tx = db.transaction('reads', 'readwrite');
      tx.objectStore('reads').put({ key, user, path, data, savedAt: Date.now() });
      // Deliberately not awaited: a cache write must never delay the read that
      // produced it, and a failure here is invisible by design.
    } catch (_) {}
  }).catch(() => {});
}

// Deletes one account's cached reads. Called on sign-out, so a shared phone does
// not keep the previous user's data on disk after the session ends.
async function clearReadCache(userID) {
  const user = userID != null ? String(userID) : readCacheUser;
  if (!user) return;
  try {
    const db = await offlineDB();
    if (!db) return;
    const tx = db.transaction('reads', 'readwrite');
    const cursorReq = tx.objectStore('reads').index('by_user').openCursor(IDBKeyRange.only(user));
    cursorReq.onsuccess = () => {
      const cursor = cursorReq.result;
      if (!cursor) return;
      cursor.delete();
      cursor.continue();
    };
    await idbDone(tx);
  } catch (_) {}
}

/* Whether the last GET came from the cache. This drives the offline banner
   alongside navigator.onLine, because a hotel proxy can answer 502 while the
   browser still believes it is online -- which is exactly when the cached copy
   is being shown and the browser's own flag would say nothing is wrong. */
function noteReadSource(cached) {
  if (cached === lastReadWasCached) return;
  lastReadWasCached = cached;
  try {
    window.dispatchEvent(new CustomEvent('lira:read-source', { detail: { cached } }));
  } catch (_) {}
}

/* ── API client ── */
async function apiFetch(path, { method='GET', body, token } = {}) {
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  let res;
  try {
    res = await fetch(API + path, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch (networkErr) {
    // No connection at all. For a read, the last good answer is what the person
    // is looking at anyway; for a write there is nothing to fall back to.
    if (method === 'GET') {
      const cached = await readCacheGet(path);
      if (cached) { noteReadSource(true); return cached.data; }
    }
    // Marked so a page made of several parallel reads can stay quiet when the
    // offline banner already explains why nothing arrived.
    try { networkErr.offline = true; } catch (_) {}
    throw networkErr;
  }

  // A proxy that is up but has no route onwards answers 502/503/504 instead of
  // failing the connection. That is still "no usable network", so a cached read
  // beats the proxy's error page.
  if (method === 'GET' && (res.status === 502 || res.status === 503 || res.status === 504)) {
    const cached = await readCacheGet(path);
    if (cached) { noteReadSource(true); return cached.data; }
  }

  // Read as text and parse by hand, because res.json() rejects an empty body with
  // a SyntaxError that is indistinguishable from a failed request. Signing out
  // answers 204 with nothing in it, and a successful sign-out must not look like
  // a broken network call to whatever asked for it.
  const raw = await res.text();
  let data = {};
  if (raw) { try { data = JSON.parse(raw); } catch (_) { data = {}; } }
  if (!res.ok) {
    const msg = data.error
      ? (typeof data.error === 'object' ? Object.values(data.error).join(', ') : data.error)
      : 'Something went wrong';
    const err = new Error(msg);
    // The STATUS travels with the error.
    //
    // Without it a caller can only tell two refusals apart by matching the English
    // sentence the server wrote -- and envelope.error is server-controlled English, so
    // matching it also breaks the EN/PT parity every other string in this file keeps.
    // The LCU pool guard needs this: deleting a reader that is in the pool is a 409
    // with a perfectly good message, and the row it belongs on has to show its own
    // error rather than the page banner nine hundred pixels away.
    err.status = res.status;
    err.reason = data.reason;
    // The server refuses work from a user who still owes an LCU result for
    // today. Both cases here are 403, so the code in the body is the only way to
    // tell them apart; the units come along so the prompt can be built from the
    // response that refused the call rather than asking again.
    if (data.code === 'lcu_test_required') {
      err.lcuRequired = true;
      err.lcuUnits = data.units || [];
      err.lcuToday  = data.today  || '';
    }
    // Sign-in is refused with 403 while a manager has asked this account to
    // change its password. The password was correct, so this is not a failure:
    // the auth screen has to notice the flag and move to the change step rather
    // than showing "incorrect email or password" for correct credentials.
    if (data.must_reset_password) {
      err.mustResetPassword = true;
    }
    throw err;
  }
  // Only successful reads are worth remembering. An error is a fact about the
  // request, not about the data.
  if (method === 'GET') {
    readCachePut(path, data);
    noteReadSource(false);
  }
  return data;
}

/* Photos are the one thing this app fetches as bytes rather than JSON.
   <img src="/v1/..."> cannot send the bearer token, so every photo is fetched
   with fetch() and turned into an object URL by the component that shows it;
   that component revokes the URL when it unmounts. */
async function apiFetchBlob(path, token) {
  const res = await fetch(API + path, {
    headers: token ? { 'Authorization': `Bearer ${token}` } : {},
  });
  if (!res.ok) {
    const err = new Error('Could not load image');
    err.status = res.status;
    throw err;
  }
  return res.blob();
}

/* The client half of the photo contract: shrink before sending.
   A phone writes a 4-8 MB JPEG with EXIF; six of those over hotel wifi is not
   an upload, it is a wait. The image is drawn onto a canvas at no more than
   1600 px on its long edge, flattened onto white (so a transparent PNG does not
   turn black), and encoded as JPEG. The browser's own EXIF orientation is
   applied by createImageBitmap/Image, and every other tag -- GPS included -- is
   discarded by the canvas.

   Throws Error('type') when the file cannot be decoded, Error('large') when it
   is over the hard cap. The caller turns those into distinct messages because
   neither can be fixed by pressing Retry. */
const MAX_PHOTO_BYTES = 8 * 1024 * 1024;
// Mirrors data.MaxAttachmentsPerIssue on the server. Six is two rows of tiles
// on a 360 px phone and one row on the desktop modal.
const MAX_PHOTOS = 6;

async function shrinkImage(file) {
  if (!file || !file.type || file.type.indexOf('image/') !== 0) throw new Error('type');
  if (file.size > 20 * 1024 * 1024) throw new Error('large');

  let source;
  try {
    if (window.createImageBitmap) {
      source = await createImageBitmap(file, { imageOrientation: 'from-image' });
    } else {
      source = await new Promise((resolve, reject) => {
        const url = URL.createObjectURL(file);
        const img = new Image();
        img.onload = () => { URL.revokeObjectURL(url); resolve(img); };
        img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('type')); };
        img.src = url;
      });
    }
  } catch (_) {
    throw new Error('type');
  }

  const sw = source.width, sh = source.height;
  if (!sw || !sh) {
    if (source.close) source.close();
    throw new Error('type');
  }
  const scale = Math.min(1, 1600 / Math.max(sw, sh));
  const w = Math.max(1, Math.round(sw * scale));
  const h = Math.max(1, Math.round(sh * scale));

  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext('2d');
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, w, h);
  ctx.drawImage(source, 0, 0, w, h);
  if (source.close) source.close();

  let blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', 0.78));
  if (!blob) throw new Error('type');
  if (blob.size > 1024 * 1024) {
    const smaller = await new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', 0.65));
    if (smaller && smaller.size < blob.size) blob = smaller;
  }
  if (blob.size > MAX_PHOTO_BYTES) throw new Error('large');
  return new File([blob], 'photo.jpg', { type: 'image/jpeg' });
}

/* Multipart upload, one photo per request. This deliberately does not use
   apiFetch: that helper forces Content-Type: application/json, which would stop
   the browser writing the multipart boundary. Setting only Authorization lets
   fetch set the boundary itself.

   clientUUID is the outbox's per-photo key. It travels as a query parameter
   rather than a form field because multipart parts arrive in order, and a field
   written after the photo would not have been read when the bytes are decoded.
   The server answers a retry with the attachment the first attempt stored. */
async function uploadIssuePhoto(issueId, file, token, clientUUID) {
  const fd = new FormData();
  fd.append('photo', file, 'photo.jpg');
  const suffix = clientUUID ? `?client_uuid=${encodeURIComponent(clientUUID)}` : '';
  const res = await fetch(`${API}/issues/${issueId}/attachments${suffix}`, {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${token}` },
    body: fd,
  });

  const raw = await res.text();
  let data = {};
  if (raw) { try { data = JSON.parse(raw); } catch (_) { data = {}; } }
  if (!res.ok) {
    const msg = data.error
      ? (typeof data.error === 'object' ? Object.values(data.error).join(', ') : data.error)
      : 'Upload failed';
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return data.attachment;
}

/* -- Outbox ----------------------------------------------------------------

   Work captured on a phone has to survive the app being closed, the phone being
   restarted and the token expiring. A row is the whole issue: the fields the
   server needs, the photos as blobs, and enough state to resume an upload that
   was interrupted.

   Two keys are mandatory, and are the reason the server grew any of this. `id`
   is sent as client_uuid on the issue and on every photo, so a retry after a
   lost response gets the row the first attempt wrote instead of a second copy.
   `createdAt` is sent as created_at, so a photo taken at 09:00 in a plant room
   is reported at 09:00 rather than at the moment the phone found signal. */

function newUUID() {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID();
    }
  } catch (_) {}
  // getRandomValues works without a secure context, unlike randomUUID. The
  // server accepts any canonical UUID; the version/variant bits are set for
  // correctness rather than to satisfy a strict parser.
  const bytes = new Uint8Array(16);
  try {
    crypto.getRandomValues(bytes);
  } catch (_) {
    for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  return hex.slice(0,8) + '-' + hex.slice(8,12) + '-' + hex.slice(12,16) + '-' + hex.slice(16,20) + '-' + hex.slice(20);
}

// RFC3339 without milliseconds, which is the shape the server stores verbatim.
function isoNow() {
  return new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
}

function outboxChanged(detail) {
  try { window.dispatchEvent(new CustomEvent('lira:outbox', { detail: detail || {} })); } catch (_) {}
}

async function outboxAll(userID) {
  try {
    const db = await offlineDB();
    if (!db) return [];
    const store = db.transaction('outbox', 'readonly').objectStore('outbox');
    const rows = store.indexNames.contains('by_user')
      ? await idbResult(store.index('by_user').getAll(String(userID)))
      : (await idbResult(store.getAll())).filter(r => String(r.user) === String(userID));
    return rows.sort((a, b) => String(a.createdAt).localeCompare(String(b.createdAt)));
  } catch (_) {
    return [];
  }
}

async function outboxAdd(row) {
  const db = await offlineDB();
  if (!db) throw new Error('This browser cannot store work offline.');
  const tx = db.transaction('outbox', 'readwrite');
  tx.objectStore('outbox').put(row);
  if (!await idbDone(tx)) throw new Error('Could not save the issue on this device.');
  outboxChanged({ added: row.id });
}

async function outboxSave(row) {
  try {
    const db = await offlineDB();
    if (!db) return;
    const tx = db.transaction('outbox', 'readwrite');
    tx.objectStore('outbox').put(row);
    await idbDone(tx);
    outboxChanged({ updated: row.id });
  } catch (_) {}
}

async function outboxDelete(id) {
  try {
    const db = await offlineDB();
    if (!db) return;
    const tx = db.transaction('outbox', 'readwrite');
    tx.objectStore('outbox').delete(id);
    await idbDone(tx);
    outboxChanged({ removed: id });
  } catch (_) {}
}

// One sync run at a time. Two runs would race each other into the same
// client_uuid, and while the server would deduplicate them, the loser would
// spend a request to learn nothing.
let outboxSyncing = false;
let outboxBlockedUntil = 0;

async function syncOutbox(session) {
  if (outboxSyncing) return;
  if (!session || !session.token || !session.user || session.user.id == null) return;
  if (typeof navigator !== 'undefined' && navigator.onLine === false) return;
  if (Date.now() < outboxBlockedUntil) return;

  outboxSyncing = true;
  const result = { sent: 0, rejected: 0, error: '', blocked: false, auth: false };
  try {
    const rows = await outboxAll(session.user.id);
    for (const row of rows) {
      if (navigator.onLine === false) break;
      // A row the server refused for its content will be refused again. Keep it
      // for the person to read and discard rather than spending an attempt every
      // minute. A row that needs a sign-in waits for a *different* token; the
      // same expired one must not stop the rows queued behind it.
      if (row.state === 'rejected') continue;
      if (row.state === 'needs-auth' && row.authToken === session.token) continue;
      const outcome = await syncOutboxRow(row, session);
      if (outcome === 'done') result.sent++;
      else if (outcome === 'rejected') result.rejected++;
      else if (outcome === 'auth') { result.auth = true; result.error = row.lastError || ''; break; }
      else if (outcome === 'blocked') { result.blocked = true; result.error = row.lastError || ''; break; }
      else if (outcome === 'error') { result.error = row.lastError || ''; break; }
    }
  } finally {
    outboxSyncing = false;
    outboxChanged({ sync: result });
  }
}

async function syncOutboxRow(row, session) {
  let issueId = row.serverId || null;

  if (!issueId) {
    try {
      const res = await apiFetch('/issues', {
        method: 'POST',
        token: session.token,
        body: {
          mode: row.mode,
          location: row.location,
          type: row.type,
          problem: row.problem,
          status: 'Pending',
          client_uuid: row.id,
          created_at: row.createdAt,
        },
      });
      issueId = res.issue && res.issue.id;
      if (!issueId) throw new Error('The server did not return an issue id.');
      row.serverId = issueId;
      row.state = 'sending';
      row.lastError = '';
      await outboxSave(row);
    } catch (ex) {
      return classifyOutboxError(row, ex, session);
    }
  }

  // The issue exists, so what remains is its photos. Each success is recorded
  // before the next attempt, so an interrupted run resumes rather than starting
  // the whole set again.
  const uploaded = new Set(row.uploadedPhotoIds || []);
  for (const photo of row.photos || []) {
    if (uploaded.has(photo.id)) continue;
    if (navigator.onLine === false) return 'error';
    try {
      const blob = photo.blob instanceof Blob
        ? photo.blob
        : new Blob([photo.blob], { type: photo.type || 'image/jpeg' });
      await uploadIssuePhoto(issueId, blob, session.token, photo.id);
      uploaded.add(photo.id);
      row.uploadedPhotoIds = Array.from(uploaded);
      await outboxSave(row);
    } catch (ex) {
      const status = ex && ex.status;
      if (status === 409) {
        // The issue already holds the maximum number of photos. Nothing else
        // can be attached, but the issue itself is good: keep it and stop.
        row.photoError = 'full';
        break;
      }
      if (status === 400 || status === 413 || status === 415 || status === 422) {
        // One unreadable photo must not strand a perfectly good issue. Drop it
        // and carry on with the rest.
        uploaded.add(photo.id);
        row.uploadedPhotoIds = Array.from(uploaded);
        row.photoError = ex.message || 'photo rejected';
        await outboxSave(row);
        continue;
      }
      return classifyOutboxError(row, ex, session);
    }
  }

  // Everything the server will take has arrived, so the row's job is done.
  await outboxDelete(row.id);
  return 'done';
}

function classifyOutboxError(row, ex, session) {
  const status = ex && ex.status;
  const message = (ex && ex.message) || 'Could not send';

  if (status === 401) {
    // The token expired while the issue waited. The work is not lost: it stays
    // queued and resumes when the same account signs in again. The token is
    // recorded so the next run with this same token skips it instead of
    // failing on it forever.
    row.state = 'needs-auth';
    row.authToken = session && session.token;
    row.lastError = message;
    outboxSave(row);
    return 'auth';
  }
  if (ex && ex.lcuRequired) {
    // A server-side gate that lifts once today's test is recorded. This is not a
    // permanent failure and must not be marked as one.
    row.state = 'pending';
    row.lastError = 'lcu_test_required';
    outboxBlockedUntil = Date.now() + 5 * 60 * 1000;
    outboxSave(row);
    return 'blocked';
  }
  if (status === 400 || status === 422) {
    row.state = 'rejected';
    row.lastError = message;
    outboxSave(row);
    return 'rejected';
  }

  // Network, proxy, 5xx: try again later. The attempt count is for the UI; the
  // trigger is connectivity and the periodic run, not an unbounded tight retry.
  row.state = 'pending';
  row.attempts = (row.attempts || 0) + 1;
  row.lastError = message;
  outboxSave(row);
  return 'error';
}


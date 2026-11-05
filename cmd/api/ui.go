package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/julienschmidt/httprouter"
)

// The frontend ships inside the binary, so the single-page app and the
// libraries it needs are all embedded here rather than fetched from a CDN at
// runtime.
//
//go:embed ui/index.html ui/vendor ui/manifest.webmanifest
//go:embed ui/icons/*.png
var uiFiles embed.FS

// Cache policy for the two kinds of frontend asset, which want opposite
// behaviour.
const (
	// The app shell changes with every release, so the browser may store it but
	// must revalidate: the ETag makes that a ~60 byte 304 instead of a fresh
	// 268 KB download on every navigation.
	uiCacheControl = "no-cache"
	// The vendored libraries are pinned to an exact version and the version is
	// part of the filename, so their contents can never change under a URL the
	// browser has already seen. Upgrading React changes the name, which is a
	// new URL, so there is no path by which a stale copy survives.
	vendorCacheControl = "public, max-age=31536000, immutable"
)

type asset struct {
	body        []byte // as stored, for clients that cannot take gzip
	gzip        []byte // the same bytes compressed, pre-built at startup
	etag        string
	contentType string
}

var (
	uiOnce     sync.Once
	uiApp      asset
	uiVendors  sync.Map // vendored filename -> asset
	uiIcons    sync.Map // icon filename -> asset
	uiManifest asset
	uiSW       asset
	uiBuild    string
)

const manifestCacheControl = "no-cache"
const iconCacheControl = "public, max-age=31536000, immutable"
const serviceWorkerCacheControl = "no-cache"

// loadUI reads the embedded frontend once and prepares it for serving.
//
// The gzip copy and the ETag are built here rather than per request: the
// payload never changes without a new binary, so compressing it on every load
// would burn CPU to produce bytes that never differ. Measured on the app shell,
// that is 268 KB down to 62 KB.
func loadUI() {
	uiOnce.Do(func() {
		uiApp = newAsset("ui/index.html", "text/html; charset=utf-8")
		uiManifest = newAsset("ui/manifest.webmanifest", "application/manifest+json; charset=utf-8")

		entries, err := fs.ReadDir(uiFiles, "ui/vendor")
		if err != nil {
			panic("lira: embedded ui/vendor is missing: " + err.Error())
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			uiVendors.Store(entry.Name(), newAsset("ui/vendor/"+entry.Name(), vendorContentType(entry.Name())))
		}

		icons, err := fs.ReadDir(uiFiles, "ui/icons")
		if err != nil {
			panic("lira: embedded ui/icons is missing: " + err.Error())
		}
		for _, entry := range icons {
			// The icon directory also holds generate.ps1, the script that draws
			// these files. It is embedded because the directory is, but it is not
			// an icon and must not be precached or served as image/png.
			if entry.IsDir() || !strings.EqualFold(path.Ext(entry.Name()), ".png") {
				continue
			}
			uiIcons.Store(entry.Name(), newAsset("ui/icons/"+entry.Name(), "image/png"))
		}

		uiBuild = uiFingerprint()
		uiSW = newGeneratedAsset([]byte(serviceWorker(uiBuild, precacheList())), "text/javascript; charset=utf-8")
	})
}

// uiFingerprint identifies this binary's frontend. It covers the shell, the
// manifest and every vendor/icon filename, so the service worker's cache name
// changes exactly when something it precaches changes.
//
// The icon and vendor maps are iterated without sorting only to feed a hash:
// the result does not depend on order because the hash is over the combined
// sorted keys below.
func uiFingerprint() string {
	// The worker template is part of the fingerprint as well as the assets it
	// caches. Without it, editing only the worker would keep the same cache name,
	// so activate would have nothing to drop and a stale precache entry could
	// survive an update.
	parts := []string{uiApp.etag, uiManifest.etag, serviceWorkerTemplate}
	for _, name := range sortedKeys(&uiVendors) {
		loaded, _ := uiVendors.Load(name)
		parts = append(parts, "v:"+name+":"+loaded.(asset).etag)
	}
	for _, name := range sortedKeys(&uiIcons) {
		loaded, _ := uiIcons.Load(name)
		parts = append(parts, "i:"+name+":"+loaded.(asset).etag)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}

func sortedKeys(m *sync.Map) []string {
	var keys []string
	m.Range(func(k, _ any) bool {
		keys = append(keys, k.(string))
		return true
	})
	sort.Strings(keys)
	return keys
}

// precacheList is every URL the shell needs to open with no network. Vendor and
// icon names come from the embedded filesystem rather than a hand-written list,
// so adding a font or an icon cannot leave the service worker behind.
func precacheList() []string {
	paths := []string{"/dashboard", "/manifest.webmanifest"}
	for _, name := range sortedKeys(&uiVendors) {
		paths = append(paths, "/vendor/"+name)
	}
	for _, name := range sortedKeys(&uiIcons) {
		paths = append(paths, "/icons/"+name)
	}
	return paths
}

// newGeneratedAsset prepares bytes this process generated rather than read from
// the embed. The ETag is still the hash of the body, so a conditional request
// works the same way as it does for the shell.
func newGeneratedAsset(data []byte, contentType string) asset {
	sum := sha256.Sum256(data)
	return asset{
		body:        data,
		gzip:        gzipBytes(data),
		etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		contentType: contentType,
	}
}

// vendorContentType says how a vendored file must be served.
//
// This was a single hardcoded JavaScript type back when the only vendored files
// were the three frontend libraries, and a browser enforces that: a woff2 sent as
// text/javascript is rejected outright, so the font silently never loads and
// nothing says why. Deriving it from the extension keeps the two facts that used
// to be the same fact -- what a file is, and how it has to be served -- from
// drifting apart the moment a fourth kind of file appears.
//
// The fonts are also the first vendored files that are not text, which is why
// alreadyCompressed below exists alongside this.
func vendorContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// alreadyCompressed reports whether a file's bytes are already in a compressed
// container, so compressing them again is work for nothing.
//
// woff2 is Brotli-compressed inside a woff2 wrapper. Gzipping it costs CPU at
// startup and at every cold build to produce bytes that are larger than what went
// in, and the response is then decoded by the browser on every load to arrive
// back where it started.
func alreadyCompressed(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".woff2", ".woff", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".zip", ".gz":
		return true
	default:
		return false
	}
}

func newAsset(name, contentType string) asset {
	data, err := fs.ReadFile(uiFiles, name)
	if err != nil {
		// Unreachable while the //go:embed directive above matches, so this can
		// only fire if someone edits the embed paths. Failing at startup beats
		// serving a broken page on every request.
		panic("lira: embedded file " + name + " is missing: " + err.Error())
	}

	sum := sha256.Sum256(data)

	var compressed []byte
	if !alreadyCompressed(name) {
		compressed = gzipBytes(data)
	}

	return asset{
		body:        data,
		gzip:        compressed,
		etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		contentType: contentType,
	}
}

// gzipBytes is BestCompression because this runs once per process and the
// frontend is the largest thing the server sends. nil only for an empty body.
func gzipBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		panic("lira: gzip writer: " + err.Error())
	}
	if _, err := zw.Write(data); err != nil {
		panic("lira: gzip write: " + err.Error())
	}
	if err := zw.Close(); err != nil {
		panic("lira: gzip close: " + err.Error())
	}
	return buf.Bytes()
}

// uiHandler serves the app shell.
func (app *application) uiHandler(w http.ResponseWriter, r *http.Request) {
	loadUI()
	uiApp.serve(w, r, uiCacheControl)
}

// vendorHandler serves the embedded frontend libraries. Keeping them in the
// binary means the UI does not stop working because a CDN is unreachable,
// blocked by the hotel's network, or reorganised its URLs.
func (app *application) vendorHandler(w http.ResponseWriter, r *http.Request) {
	loadUI()
	params := httprouter.ParamsFromContext(r.Context())
	name := params.ByName("file")

	loaded, ok := uiVendors.Load(name)
	if !ok {
		app.notFoundResponse(w, r)
		return
	}
	loaded.(asset).serve(w, r, vendorCacheControl)
}

// serviceWorker renders the generated worker for this build.
//
// The build fingerprint and the precache list are baked into the source rather
// than fetched at runtime, because a worker that has to ask the network what to
// cache cannot install on the first visit to a flaky hotel wifi. Bumping the
// fingerprint changes CACHE, which is also how the activate handler knows which
// old cache to drop.
func serviceWorker(build string, precache []string) string {
	var list strings.Builder
	for i, url := range precache {
		if i > 0 {
			list.WriteString(",\n  ")
		}
		list.WriteString(strconv.Quote(url))
	}

	return strings.NewReplacer(
		"__BUILD__", build,
		"__PRECACHE__", list.String(),
	).Replace(serviceWorkerTemplate)
}

// The worker deliberately caches the shell, never the API.
//
// Reads from /v1 are cached in IndexedDB by the app, where the cache can be
// namespaced per account and invalidated by the same code that writes it. An
// HTTP cache in the middle would be a second, invisible source of truth that
// outlives a sign-out. Writes go through the outbox, which owns retry, so the
// worker has nothing useful to do with them.
//
// Template substitutions are __BUILD__ and __PRECACHE__; there are no backticks
// in the script so it can live in a Go raw string.
const serviceWorkerTemplate = `// Generated by the LIRA server. Do not edit: cmd/api/ui.go owns this file.
const BUILD  = '__BUILD__';
const CACHE  = 'lira-shell-' + BUILD;
const SHELL  = '/dashboard';
const PRECACHE = [
  __PRECACHE__
];

self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE);
    // Individually rather than addAll: addAll rejects the whole install when a
    // single URL 404s, and a worker that will not install leaves no offline
    // shell at all. One missing icon should cost one icon.
    await Promise.all(PRECACHE.map(async url => {
      try {
        const res = await fetch(new Request(url, { cache: 'reload' }));
        if (res && res.ok) await cache.put(url, res);
      } catch (err) {
        // Offline during install, or a missing asset. The next update retries.
      }
    }));
  })());
  // No skipWaiting here on purpose. A new worker taking over mid-session swaps
  // the running app for one built from different code, which is exactly the
  // kind of surprise a form full of unsaved work does not need. The page shows
  // a "new version" banner and sends SKIP_WAITING when the user accepts.
});

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.map(name => {
      if (name !== CACHE && name.indexOf('lira-shell-') === 0) return caches.delete(name);
      return Promise.resolve(false);
    }));
    // Take control of already-open tabs so the new build is used without a
    // second manual reload.
    await self.clients.claim();
  })());
});

self.addEventListener('message', event => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('fetch', event => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  // The API belongs to the app, not to the HTTP cache. See the comment above.
  if (url.pathname.indexOf('/v1/') === 0) return;
  // The worker script itself is managed by the browser's update check.
  if (url.pathname === '/sw.js') return;

  if (url.pathname.indexOf('/vendor/') === 0 ||
      url.pathname.indexOf('/icons/') === 0 ||
      url.pathname === '/manifest.webmanifest') {
    event.respondWith(cacheFirst(req));
    return;
  }

  // Everything else same-origin is network-first with the cache as a fallback.
  // A navigation offline gets the shell. The shell is stored under /dashboard,
  // so a route the phone has never visited still opens the app rather than the
  // browser's own offline page.
  event.respondWith(networkThenCache(req));
});

async function networkThenCache(req) {
  const cache = await caches.open(CACHE);
  const navigate = req.mode === 'navigate';
  const key = navigate ? SHELL : req;
  try {
    const res = await fetch(req);
    if (res && res.ok && res.type === 'basic') {
      await cache.put(key, res.clone());
      return res;
    }
    // A hotel proxy or captive portal answers 502/503/504 rather than dropping
    // the connection. That is still "no usable network", so the cached copy is
    // better than the proxy's error page. A 500 is left alone: that is the app
    // itself answering, and masking it would hide a real fault.
    if (res && (res.status === 502 || res.status === 503 || res.status === 504)) {
      const cached = await cache.match(key);
      if (cached) return cached;
    }
    return res;
  } catch (err) {
    const cached = await cache.match(key);
    if (cached) return cached;
    return new Response('LIRA is offline and has not finished installing.', {
      status: 503,
      headers: { 'Content-Type': 'text/plain; charset=utf-8' },
    });
  }
}

// Versioned assets are immutable, so the cache answers first and the network is
// only consulted for something this build has not seen.
async function cacheFirst(req) {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res && res.ok) await cache.put(req, res.clone());
  return res;
}
`

// manifestHandler serves the web app manifest. no-cache rather than immutable:
// the manifest is not fingerprinted, so a stale copy would keep the old icon and
// start_url after a deploy.
func (app *application) manifestHandler(w http.ResponseWriter, r *http.Request) {
	loadUI()
	uiManifest.serve(w, r, manifestCacheControl)
}

// serviceWorkerHandler serves the worker at the origin root, which is what lets
// it control the whole app. Service-Worker-Allowed makes that explicit for any
// browser that also considers the script's own path.
func (app *application) serviceWorkerHandler(w http.ResponseWriter, r *http.Request) {
	loadUI()
	w.Header().Set("Service-Worker-Allowed", "/")
	uiSW.serve(w, r, serviceWorkerCacheControl)
}

// iconHandler serves the PWA icons. They are immutable: a changed icon is a new
// filename and a new manifest, never a new body under an old URL.
func (app *application) iconHandler(w http.ResponseWriter, r *http.Request) {
	loadUI()
	params := httprouter.ParamsFromContext(r.Context())
	name := params.ByName("file")

	loaded, ok := uiIcons.Load(name)
	if !ok {
		app.notFoundResponse(w, r)
		return
	}
	loaded.(asset).serve(w, r, iconCacheControl)
}

// serve writes one asset, compressing it when the client accepts gzip and
// answering conditional requests before doing any compression work.
func (a asset) serve(w http.ResponseWriter, r *http.Request, cacheControl string) {
	h := w.Header()
	h.Set("ETag", a.etag)
	// The same bytes go out in two encodings, so caches have to key on which
	// one the client can accept.
	h.Set("Vary", "Accept-Encoding")
	h.Set("Cache-Control", cacheControl)

	// Checked before compressing so the common "nothing has changed" reply is
	// cheap even when the body is large.
	if etagMatches(r.Header.Get("If-None-Match"), a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body := a.body
	if a.gzip != nil && acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		body = a.gzip
	}
	h.Set("Content-Type", a.contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// etagMatches reports whether an If-None-Match header covers the current ETag.
// A weak validator counts: for a byte-identical body there is nothing a weak
// comparison would wrongly accept.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// acceptsGzip reports whether the client will take a gzip-encoded body. The
// q=0 case is honoured because "gzip;q=0" is a client explicitly saying it
// does not want it, and sending it anyway breaks the request.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		params = strings.TrimSpace(params)
		if q, ok := strings.CutPrefix(params, "q="); ok {
			if weight, err := strconv.ParseFloat(strings.TrimSpace(q), 64); err == nil && weight == 0 {
				return false
			}
		}
		return true
	}
	return false
}

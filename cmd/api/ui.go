package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/julienschmidt/httprouter"
)

// The frontend ships inside the binary, so the single-page app and the
// libraries it needs are all embedded here rather than fetched from a CDN at
// runtime.
//
//go:embed ui/index.html ui/vendor
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
	uiOnce    sync.Once
	uiApp     asset
	uiVendors sync.Map // vendored filename -> asset
)

// loadUI reads the embedded frontend once and prepares it for serving.
//
// The gzip copy and the ETag are built here rather than per request: the
// payload never changes without a new binary, so compressing it on every load
// would burn CPU to produce bytes that never differ. Measured on the app shell,
// that is 268 KB down to 62 KB.
func loadUI() {
	uiOnce.Do(func() {
		uiApp = newAsset("ui/index.html", "text/html; charset=utf-8")
		entries, err := fs.ReadDir(uiFiles, "ui/vendor")
		if err != nil {
			panic("lira: embedded ui/vendor is missing: " + err.Error())
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			uiVendors.Store(entry.Name(), newAsset("ui/vendor/"+entry.Name(), "text/javascript; charset=utf-8"))
		}
	})
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

	// BestCompression rather than the default: this runs once per process, and
	// the frontend is by far the largest thing the server sends.
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

	return asset{
		body:        data,
		gzip:        buf.Bytes(),
		etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		contentType: contentType,
	}
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

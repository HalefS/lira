package report

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrNoBrowser is returned by NewBrowser when no Chromium-based browser could
// be located. It is deliberately not fatal: the rest of the application works
// fine without a PDF renderer, so the server starts regardless and only the
// PDF endpoint reports the problem.
var ErrNoBrowser = errors.New("no chromium-based browser found")

// renderTimeout bounds a single print-to-PDF run. Chrome normally finishes in
// well under a second; the ceiling exists so a wedged browser process can't
// hold an HTTP request open indefinitely.
const renderTimeout = 30 * time.Second

// virtualTimeBudget is how long Chrome is allowed to advance its own clock
// before snapshotting the page, in milliseconds. The report template has no
// JavaScript, so this exists purely to give the web font time to arrive —
// without it Chrome can snapshot before the font loads and the PDF falls back
// to a system face. Chrome fast-forwards this, so it costs nothing when the
// font is already cached or the host is offline.
const virtualTimeBudget = 3000

// chromeLookupNames are the executable names tried on PATH, in order. Any
// Chromium-based browser works — Chrome, Chromium, Edge, Brave and the
// standalone chrome-headless-shell all implement --print-to-pdf identically.
var chromeLookupNames = []string{
	"chrome",
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
	"msedge",
	"microsoft-edge",
	"chrome-headless-shell",
}

// chromeWellKnownPaths are absolute locations to try when the browser isn't on
// PATH, which is the normal case for a GUI install on Windows and macOS and for
// a package install on Linux.
var chromeWellKnownPaths = map[string][]string{
	"windows": {
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	},
	"darwin": {
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/Microsoft Edge",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	},
	"linux": {
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/snap/bin/chromium",
		"/usr/bin/microsoft-edge",
		"/usr/bin/brave-browser",
	},
}

// Browser turns an HTML document into a PDF by handing it to a headless
// Chromium and letting the browser's own print pipeline lay it out.
//
// Driving the browser's `--print-to-pdf` flag rather than talking to the
// DevTools protocol is what keeps this package dependency-free: the report is
// static HTML and CSS, so there is nothing to wait for beyond the font, and
// `os/exec` covers the rest.
type Browser struct {
	// bin is the absolute path of the browser executable, resolved once at
	// startup so a missing browser is reported when the server boots rather
	// than on a user's download.
	bin string
}

// NewBrowser locates a Chromium-based browser. A non-empty configured path is
// used as-is (and must exist), which is how an operator points the server at a
// specific build; otherwise PATH and the well-known install locations are
// searched.
func NewBrowser(configured string) (*Browser, error) {
	if configured != "" {
		abs, err := filepath.Abs(configured)
		if err != nil {
			return nil, fmt.Errorf("report: resolving browser path %q: %w", configured, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("report: browser %q: %w", configured, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("report: browser %q is a directory, not an executable", configured)
		}
		return &Browser{bin: abs}, nil
	}

	for _, name := range chromeLookupNames {
		if path, err := exec.LookPath(name); err == nil {
			return &Browser{bin: path}, nil
		}
	}

	for _, path := range chromeWellKnownPaths[runtime.GOOS] {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return &Browser{bin: path}, nil
		}
	}

	return nil, ErrNoBrowser
}

// Path returns the browser executable in use, for logging at startup.
func (b *Browser) Path() string { return b.bin }

// Render converts an HTML document to PDF bytes.
//
// The document is written to a temporary directory of its own rather than
// printed from a URL served by this process: it keeps the render independent
// of the app's own listener, and it means each run gets a private
// --user-data-dir, without which two concurrent report downloads would fight
// over Chrome's single-instance profile lock and one of them would fail.
func (b *Browser) Render(ctx context.Context, html string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "lira-report-")
	if err != nil {
		return nil, fmt.Errorf("report: creating temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "report.html")
	if err := os.WriteFile(source, []byte(html), 0o600); err != nil {
		return nil, fmt.Errorf("report: writing html: %w", err)
	}

	output := filepath.Join(dir, "report.pdf")

	// Chrome reports success on stderr, so its output is captured for the error
	// message and never treated as a failure on its own.
	cmd := exec.CommandContext(ctx, b.bin,
		"--headless",
		"--disable-gpu",
		// The document is generated locally from escaped template data and
		// loads no scripts, so the sandbox protects nothing here — but it is
		// also what lets this run as root inside a container, which is a
		// common way to deploy a single static binary.
		"--no-sandbox",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-sync",
		// Page geometry comes from the template's @page rule; the browser's own
		// header and footer would otherwise be printed on top of it.
		"--no-pdf-header-footer",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		fmt.Sprintf("--virtual-time-budget=%d", virtualTimeBudget),
		"--print-to-pdf="+output,
		"file://"+filepath.ToSlash(source),
	)
	// Chrome is chatty on stderr even when it succeeds.
	cmd.Stdout = nil
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("report: browser timed out after %s", renderTimeout)
		}
		return nil, fmt.Errorf("report: browser failed: %w: %s", err, lastLine(string(stderr)))
	}

	pdf, err := os.ReadFile(output)
	if err != nil {
		// Chrome exited 0 without writing a file when it was killed mid-run or
		// when the profile directory was not writable.
		return nil, fmt.Errorf("report: browser produced no output: %s", lastLine(string(stderr)))
	}
	if len(pdf) == 0 {
		return nil, errors.New("report: browser produced an empty file")
	}
	return pdf, nil
}

// lastLine trims a browser's multi-line diagnostics down to something worth
// putting in an error message.
func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

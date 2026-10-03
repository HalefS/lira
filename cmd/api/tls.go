package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// TLS support.
//
// The server serves HTTPS when it is given a certificate and a key, and plain HTTP
// when it is not, so a single binary covers both a deployment that terminates TLS
// here and one sitting behind a reverse proxy. Which of the two actually happened
// is always logged at startup, because an operator who expects HTTPS and forgets
// the flags would otherwise be handed a silently downgraded server with nothing to
// say so.
//
// The certificate is never generated here. Minting a self-signed one looks like it
// removes a step, but it moves the cost onto everyone who uses the app -- a browser
// interstitial on every device, worsening as the certificate ages -- and it would
// leave the application storing key material. Taking cert and key paths keeps that
// decision with the operator, where a public or internal CA can be used instead.

const (
	// The environment names mirror the flags so a deployment can be configured
	// without arguments, the same way LIRADB_DSN and LIRA_CHROME_BIN already are.
	tlsCertEnv = "LIRA_TLS_CERT"
	tlsKeyEnv  = "LIRA_TLS_KEY"

	// tlsExpiryWarning is how far ahead of expiry a still-valid certificate is
	// called out. Past this point the server works fine but has a deadline, and
	// startup is the one place an operator is guaranteed to read the log.
	tlsExpiryWarning = 30 * 24 * time.Hour
)

// resolveTLS decides whether this process serves HTTPS.
//
// Every failure here is fatal by design. Half a configuration -- a certificate
// without its key, a path that does not exist, a certificate that has expired --
// means the operator believes they have HTTPS and does not, and continuing to serve
// plain HTTP in that state would hide the mistake behind a working login page. A
// refusal at boot is a far cheaper way to find out than a technician in a hotel
// basement discovering it.
//
// Returns empty paths when TLS is off, which is not an error: that is how a
// deployment behind a reverse proxy runs.
func resolveTLS(cfg config, logger *slog.Logger) (certFile, keyFile string, err error) {
	certFile, keyFile = cfg.tls.cert, cfg.tls.key

	switch {
	case certFile == "" && keyFile == "":
		return "", "", nil
	case certFile == "":
		return "", "", errors.New("-tls-key was given without -tls-cert: serving https needs both, and neither can be derived from the other")
	case keyFile == "":
		return "", "", errors.New("-tls-cert was given without -tls-key: serving https needs both, and neither can be derived from the other")
	}

	// Loaded here rather than left to ListenAndServeTLS so that an unreadable,
	// malformed or mismatched pair is reported in the same words as everything
	// else at startup, with the path that was actually tried.
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return "", "", fmt.Errorf("loading tls certificate: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return "", "", errors.New("loading tls certificate: the file chain is empty")
	}

	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", "", fmt.Errorf("parsing tls certificate: %w", err)
	}

	// Checked here, not left to the client. An expired certificate fails every
	// request, so learning about it from a browser is strictly worse than being
	// told at boot.
	now := time.Now()
	if now.After(leaf.NotAfter) {
		return "", "", fmt.Errorf("the tls certificate expired on %s", leaf.NotAfter.Format(time.DateOnly))
	}
	if now.Add(tlsExpiryWarning).After(leaf.NotAfter) {
		logger.Warn("tls certificate expires soon: renew it before this date or the server will refuse to start",
			"expires", leaf.NotAfter.Format(time.DateOnly),
			"days_left", int(time.Until(leaf.NotAfter).Hours()/24))
	}

	logger.Info("tls certificate loaded",
		"subject", leaf.Subject.CommonName,
		"issuer", leaf.Issuer.CommonName,
		"not_after", leaf.NotAfter.Format(time.DateOnly),
		"dns_names", leaf.DNSNames,
		"ip_addresses", leaf.IPAddresses)

	return certFile, keyFile, nil
}

// validateTLSFlags catches the flag combinations that would otherwise fail as a
// confusing bind error or a redirect loop much later.
func validateTLSFlags(cfg config) error {
	if cfg.tls.redirectPort == cfg.port {
		return fmt.Errorf("-tls-redirect-port must differ from -port (%d): one listener cannot be both the https server and its own redirect", cfg.port)
	}
	return nil
}

// serveTLSRedirect answers every plain HTTP request with a permanent redirect to
// the same path over HTTPS, so a bookmarked http:// link or an address typed by
// hand ends up on the right scheme.
//
// Only meaningful when this process is the one terminating TLS. Behind a reverse
// proxy the plain request never reaches here -- the proxy sees it -- so the redirect
// has to be configured there instead, and running this listener in that setup only
// opens a second, redundant way in. That is why the port is opt-in rather than
// defaulted.
func serveTLSRedirect(redirectPort, httpsPort int, logger *slog.Logger) {
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", redirectPort),
		Handler: tlsRedirectHandler(httpsPort),
		// Short timeouts: this listener answers one redirect and nothing else, so
		// there is no reason to hold a connection open.
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.Info("plain http redirect listener starting",
		"addr", srv.Addr, "redirects_to", fmt.Sprintf("https://<host>:%d", httpsPort))

	if err := srv.ListenAndServe(); err != nil {
		// The https server is the one that matters. A redirect listener that
		// cannot bind -- usually port 80 already taken by something else -- is
		// worth reporting but is not a reason to take the application down.
		logger.Error("plain http redirect listener stopped", "error", err)
	}
}

// tlsRedirectHandler builds the redirect.
//
// 308 rather than 301: a permanent redirect that preserves the request method. A
// 301 on a POST is downgraded to a GET by most clients, so a form submitted to the
// wrong scheme would arrive at the right place having quietly lost its body.
func tlsRedirectHandler(httpsPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The Host header decides where the user is sent, because that is the name
		// they reached us by. Any port it carries is dropped: it is the port they
		// happened to use, and the destination is this host on the https port.
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		target := url.URL{
			Scheme:   "https",
			Host:     net.JoinHostPort(host, strconv.Itoa(httpsPort)),
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		}
		http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
	})
}

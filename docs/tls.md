# TLS

LIRA serves HTTPS when it is given a certificate and a key, and plain HTTP when it
is not. The same binary covers both a deployment that terminates TLS itself and one
that sits behind a reverse proxy.

The startup log always states which of the two is in effect:

```
level=INFO msg="starting server" addr=:4000 scheme=https env=production
level=INFO msg="starting server" addr=:4000 scheme=http  env=production
```

That line is the point of the design. `addr=:4000` reads identically either way, so
an operator who expects HTTPS and forgets the flags would otherwise have no way to
tell from the outside that the server came up unencrypted.

---

## Two ways to deploy

### The app terminates TLS

Give it a certificate and a key. It serves HTTPS on `-port` and can redirect plain
HTTP on a second port.

```
lira -port=4000 \
     -tls-cert=/etc/lira/tls/fullchain.pem \
     -tls-key=/etc/lira/tls/privkey.pem \
     -tls-redirect-port=80
```

### A reverse proxy terminates TLS

Leave the TLS flags unset and bind the app to loopback. The proxy owns the
certificate, its renewal and the redirect.

```
# The app
lira -port=4000

# Caddy, in front
lira.example.com {
    reverse_proxy 127.0.0.1:4000
}
```

Do **not** set `-tls-redirect-port` in this shape. The plain request never reaches
the app — the proxy sees it — so the redirect has to be configured on the proxy, and
enabling it here only opens a second, redundant way in. The app logs a warning if
you do.

---

## Flags and environment variables

| Flag | Environment variable | Default | Meaning |
|---|---|---|---|
| `-tls-cert` | `LIRA_TLS_CERT` | — | PEM certificate path. Enables HTTPS together with `-tls-key`. |
| `-tls-key` | `LIRA_TLS_KEY` | — | PEM private key path. |
| `-tls-redirect-port` | — | `0` (off) | Plain HTTP port that answers every request with a `308` to the HTTPS port. |

The environment names exist so a systemd unit or a container image can be configured
without arguments, the same way `LIRADB_DSN` and `LIRA_CHROME_BIN` already are.

---

## What the app refuses to do

Every one of these stops the process at startup with a message naming the problem,
rather than starting a server that is not what the operator asked for:

- `-tls-cert` without `-tls-key`, or the reverse
- a certificate or key file that does not exist, or cannot be read
- a malformed certificate, or an empty chain
- a certificate paired with the wrong private key
- an **expired** certificate
- `-tls-redirect-port` equal to `-port`, which would be a redirect loop

A certificate that is valid but expires within 30 days logs a warning and starts
normally:

```
level=WARN msg="tls certificate expires soon: renew it before this date or the server will refuse to start" \
      expires=2026-11-02 days_left=30
```

Refusing to start on an expired certificate is deliberate. An expired certificate
fails every single request, and finding that out from a technician in a hotel
basement is a much worse way to learn it than being told at boot.

### Renewing

There is no automatic reload. Renew the certificate and restart the service. The
30-day warning exists so that restart is a planned event rather than a discovery.

The redirect answers with **308 Permanent Redirect** rather than 301, because 308
preserves the request method. A 301 on a `POST` is downgraded to a `GET` by most
clients, so a form submitted to the wrong scheme would arrive having quietly lost
its body.

---

## Unaffected by TLS

Worth stating, because these are the parts that usually are not:

- **The frontend.** Every API call is built from the relative constant `/v1`, so a
  page served over HTTPS calls the API over HTTPS automatically. There is no
  absolute URL and no protocol check anywhere in it.
- **PDF reports.** The renderer writes the report HTML to a temporary file and
  prints it with `file://`. It never addresses the app's own listener, so enabling
  TLS cannot affect it.
- **Authentication.** Sessions are bearer tokens held in `localStorage`. There are
  no cookies, so there is no `Secure` attribute to set and no cookie-based CSRF
  surface that TLS would change.

---

## HSTS

Not set. `Strict-Transport-Security` is deliberately absent, because it is hard to
undo: once a browser has cached the policy it refuses plain HTTP for that host
regardless of what the server does, until the policy expires or the user clears it.

If you want it, add it to the responses served over TLS:

```go
w.Header().Set("Strict-Transport-Security", "max-age=31536000")
```

Enable it only once HTTPS is confirmed working for every client that will use the
app, and only for a hostname you control — an IP address is not a valid HSTS host.

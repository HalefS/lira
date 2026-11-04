package main

import (
	"net/http"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// Notification centre handlers.
//
// There is no notification table to create, update or delete. Every item is
// derived at read time from the domain tables; these endpoints read that feed
// and write the caller's own read marks. That is why the mark actions are POSTs
// with an item identity rather than PATCHes against a row id.
//
// GET /v1/notifications deliberately does not call RecurringAlerts.Sync() or
// TVSwapAlerts.Sync(). A read endpoint that writes is unsafe to retry and makes
// every badge refresh take locks; the alert sweeps stay owned by GET /v1/alerts,
// which is the page that renders and resolves them. The trade is that the two
// alert kinds are as fresh as the last Alerts visit, which is also when they
// become actionable.

func (app *application) listNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	user := app.contextGetUser(r)

	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	v := validator.New()

	kind := data.NotificationKind(r.URL.Query().Get("kind"))
	v.Check(kind == "" || kind.Valid(), "kind", "must be a known notification kind")

	unreadOnly := false
	switch r.URL.Query().Get("unread") {
	case "", "false":
	case "true":
		unreadOnly = true
	default:
		v.AddError("unread", "must be true or false")
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	limit := app.readInt(r.URL.Query(), "limit", data.DefaultNotificationLimit)
	capped := false
	if limit < 1 {
		limit = data.DefaultNotificationLimit
	}
	if limit > data.MaxNotificationLimit {
		limit = data.MaxNotificationLimit
		capped = true
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	items, hasMore, err := app.models.Notifications.Feed(data.FeedParams{
		UserID:     user.ID,
		IsManager:  user.Role == "manager",
		Now:        now,
		Today:      today,
		SLAMinutes: settings.SLAMinutes,
		Limit:      limit,
		Kind:       kind,
		UnreadOnly: unreadOnly,
	})
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Over the whole visible window, never the page. A badge computed from the
	// loaded rows stops at the request limit; one computed from an unread-filtered
	// response reads zero the moment somebody looks.
	unread, err := app.models.Notifications.UnreadCount(
		user.ID, user.Role == "manager", settings.SLAMinutes, now, today)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"notifications": items,
		"unread_count":  unread,
		"limit":         limit,
		"limit_capped":  capped,
		"has_more":      hasMore,
		"generated_at":  now,
	}, noStoreHeaders())
}

func (app *application) listNotificationDigestHandler(w http.ResponseWriter, r *http.Request) {
	user := app.contextGetUser(r)

	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	digest, err := app.models.Notifications.Digest(
		user.ID, user.Role == "manager", settings.SLAMinutes, time.Now())
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"digest": digest}, noStoreHeaders())
}

func (app *application) markNotificationReadHandler(w http.ResponseWriter, r *http.Request) {
	var ref data.NotificationRef
	if err := app.readJSON(w, r, &ref); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	data.ValidateNotificationRef(v, ref)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	now := time.Now()

	readAt, err := app.models.Notifications.MarkRead(user.ID, ref, now)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Recomputed rather than decremented on the client: another tab may have
	// marked something, and a local decrement drifts the moment two happen.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	unread, err := app.models.Notifications.UnreadCount(
		user.ID, user.Role == "manager", settings.SLAMinutes, now, today)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"read_at":      readAt,
		"unread_count": unread,
	}, noStoreHeaders())
}

func (app *application) markAllNotificationsReadHandler(w http.ResponseWriter, r *http.Request) {
	// Deliberately no readJSON: this endpoint takes no body, and the existing
	// decoder would reject an empty one.
	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	now := time.Now()

	readAt, err := app.models.Notifications.MarkAllRead(user.ID, now)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	unread, err := app.models.Notifications.UnreadCount(
		user.ID, user.Role == "manager", settings.SLAMinutes, now, today)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"read_at":      readAt,
		"unread_count": unread,
	}, noStoreHeaders())
}

// noStoreHeaders: a cached badge is worse than no badge.
func noStoreHeaders() http.Header {
	return http.Header{"Cache-Control": []string{"no-store"}}
}

package main

import (
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
)

func (app *application) routes() http.Handler {
	router := httprouter.New()

	// API 404 → JSON; everything else → redirect to /dashboard
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1") {
			app.notFoundResponse(w, r)
			return
		}
		http.Redirect(w, r, "/dashboard", http.StatusMovedPermanently)
	})
	router.MethodNotAllowed = http.HandlerFunc(app.methodNotAllowedResponse)

	// UI
	router.HandlerFunc(http.MethodGet, "/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusMovedPermanently)
	})
	router.HandlerFunc(http.MethodGet, "/dashboard", app.uiHandler)

	// Health
	router.HandlerFunc(http.MethodGet, "/v1/healthcheck", app.healthcheckHandler)

	// Auth
	router.HandlerFunc(http.MethodPost, "/v1/users", app.registerUserHandler)
	router.HandlerFunc(http.MethodPost, "/v1/tokens/authentication", app.createAuthTokenHandler)

	// Current user profile — /v1/profile to avoid conflict with /v1/users/:id
	router.HandlerFunc(http.MethodGet, "/v1/profile", app.requireAuth(app.getMeHandler))
	router.HandlerFunc(http.MethodPatch, "/v1/profile", app.requireAuth(app.updateMeHandler))
	router.HandlerFunc(http.MethodGet, "/v1/profile/stats", app.requireAuth(app.getMeStatsHandler))
	router.HandlerFunc(http.MethodGet, "/v1/profile/issues", app.requireAuth(app.getMeIssuesHandler))

	// Users (protected)
	router.HandlerFunc(http.MethodGet, "/v1/users", app.requireAuth(app.listUsersHandler))
	router.HandlerFunc(http.MethodGet, "/v1/users/:id", app.requireAuth(app.getUserHandler))
	router.HandlerFunc(http.MethodPatch, "/v1/users/:id/role", app.requireAuth(app.requireManager(app.updateUserRoleHandler)))
	router.HandlerFunc(http.MethodPatch, "/v1/users/:id/deactivate", app.requireAuth(app.requireManager(app.deactivateUserHandler)))

	// Issues (protected)
	router.HandlerFunc(http.MethodGet, "/v1/issues", app.requireAuth(app.listIssuesHandler))
	router.HandlerFunc(http.MethodPost, "/v1/issues", app.requireAuth(app.createIssueHandler))
	router.HandlerFunc(http.MethodGet, "/v1/issues/:id", app.requireAuth(app.showIssueHandler))
	router.HandlerFunc(http.MethodPatch, "/v1/issues/:id", app.requireAuth(app.updateIssueHandler))
	router.HandlerFunc(http.MethodDelete, "/v1/issues/:id", app.requireAuth(app.deleteIssueHandler))

	// Issue types — list for all authenticated users; mutate for managers only
	router.HandlerFunc(http.MethodGet, "/v1/issue-types", app.requireAuth(app.listIssueTypesHandler))
	router.HandlerFunc(http.MethodPost, "/v1/issue-types", app.requireAuth(app.requireManager(app.createIssueTypeHandler)))
	router.HandlerFunc(http.MethodDelete, "/v1/issue-types/:id", app.requireAuth(app.requireManager(app.deleteIssueTypeHandler)))

	// Stats (protected)
	router.HandlerFunc(http.MethodGet, "/v1/stats", app.requireAuth(app.statsHandler))

	// Reports (protected)
	router.HandlerFunc(http.MethodGet, "/v1/reports/daily", app.requireAuth(app.dailyReportHandler))

	return app.recoverPanic(app.enableCORS(app.rateLimit(app.authenticate(router))))
}

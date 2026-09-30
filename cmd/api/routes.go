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
	// Frontend libraries, embedded in the binary. These paths are not API
	// paths, so they have to be registered explicitly or the catch-all
	// redirects them to /dashboard and hands HTML back to a <script> tag.
	router.HandlerFunc(http.MethodGet, "/vendor/:file", app.vendorHandler)

	// Health
	router.HandlerFunc(http.MethodGet, "/v1/healthcheck", app.healthcheckHandler)

	// System status — CPU/RAM/DB latency for the System page (manager only)
	router.HandlerFunc(http.MethodGet, "/v1/system/status", app.requireAuth(app.requireManager(app.systemStatusHandler)))

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

	// Recurring-issue check (protected) — used while logging a new issue.
	// Named issue-duplicates (not nested under /v1/issues/) to avoid
	// colliding with the /v1/issues/:id wildcard route above.
	router.HandlerFunc(http.MethodGet, "/v1/issue-duplicates", app.requireAuth(app.checkDuplicateIssuesHandler))

	// Alerts — currently-recurring issue groups, for the Alerts page
	router.HandlerFunc(http.MethodGet, "/v1/alerts", app.requireAuth(app.listAlertsHandler))
	router.HandlerFunc(http.MethodPatch, "/v1/alerts/:id/solve", app.requireAuth(app.requireManager(app.solveAlertHandler)))
	router.HandlerFunc(http.MethodDelete, "/v1/alerts/:id", app.requireAuth(app.requireManager(app.deleteAlertHandler)))

	// Analytics — week/month comparisons, for the Analytics page
	router.HandlerFunc(http.MethodGet, "/v1/analytics", app.requireAuth(app.getAnalyticsHandler))

	// Consumables — inventory usage tracking (what was used on which issue)
	router.HandlerFunc(http.MethodGet, "/v1/consumables", app.requireAuth(app.listConsumablesHandler))

	// Consumable items — the catalog technicians pick from when logging an
	// issue; list for all authenticated users, mutate for managers only
	router.HandlerFunc(http.MethodGet, "/v1/consumable-items", app.requireAuth(app.listConsumableItemsHandler))
	router.HandlerFunc(http.MethodPost, "/v1/consumable-items", app.requireAuth(app.requireManager(app.createConsumableItemHandler)))
	router.HandlerFunc(http.MethodPatch, "/v1/consumable-items/:id/icon", app.requireAuth(app.requireManager(app.updateConsumableItemIconHandler)))
	// Inventory — managers change the counts, everyone can read the history
	router.HandlerFunc(http.MethodPatch, "/v1/consumable-items/:id/stock", app.requireAuth(app.requireManager(app.adjustConsumableItemStockHandler)))
	router.HandlerFunc(http.MethodPatch, "/v1/consumable-items/:id/reorder-level", app.requireAuth(app.requireManager(app.updateConsumableItemReorderLevelHandler)))
	router.HandlerFunc(http.MethodGet, "/v1/consumable-stock-movements", app.requireAuth(app.listStockMovementsHandler))
	router.HandlerFunc(http.MethodDelete, "/v1/consumable-items/:id", app.requireAuth(app.requireManager(app.deleteConsumableItemHandler)))

	// Third-party support — how long Telnet / Telefonica took on each handover,
	// with per-company statistics. Read-only here: handovers are edited through
	// the issue they belong to, so the owner-or-manager check lives in one place.
	router.HandlerFunc(http.MethodGet, "/v1/support-requests", app.requireAuth(app.listSupportRequestsHandler))

	// Settings — readable by any authenticated user, editable by managers only
	router.HandlerFunc(http.MethodGet, "/v1/settings", app.requireAuth(app.getSettingsHandler))
	router.HandlerFunc(http.MethodPatch, "/v1/settings", app.requireAuth(app.requireManager(app.updateSettingsHandler)))

	// Issue types — list for all authenticated users; mutate for managers only
	router.HandlerFunc(http.MethodGet, "/v1/issue-types", app.requireAuth(app.listIssueTypesHandler))
	router.HandlerFunc(http.MethodPost, "/v1/issue-types", app.requireAuth(app.requireManager(app.createIssueTypeHandler)))
	router.HandlerFunc(http.MethodPatch, "/v1/issue-types/:id/color", app.requireAuth(app.requireManager(app.updateIssueTypeColorHandler)))
	router.HandlerFunc(http.MethodDelete, "/v1/issue-types/:id", app.requireAuth(app.requireManager(app.deleteIssueTypeHandler)))

	// Departments — list for all authenticated users; mutate for managers only
	router.HandlerFunc(http.MethodGet, "/v1/departments", app.requireAuth(app.listDepartmentsHandler))
	router.HandlerFunc(http.MethodPost, "/v1/departments", app.requireAuth(app.requireManager(app.createDepartmentHandler)))
	router.HandlerFunc(http.MethodDelete, "/v1/departments/:id", app.requireAuth(app.requireManager(app.deleteDepartmentHandler)))

	// Melia Connecta Agents — list for all authenticated users; mutate for managers only
	router.HandlerFunc(http.MethodGet, "/v1/connecta-agents", app.requireAuth(app.listConnectaAgentsHandler))
	router.HandlerFunc(http.MethodPost, "/v1/connecta-agents", app.requireAuth(app.requireManager(app.createConnectaAgentHandler)))
	router.HandlerFunc(http.MethodDelete, "/v1/connecta-agents/:id", app.requireAuth(app.requireManager(app.deleteConnectaAgentHandler)))

	// Stats (protected)
	router.HandlerFunc(http.MethodGet, "/v1/stats", app.requireAuth(app.statsHandler))

	// Reports (protected)
	router.HandlerFunc(http.MethodGet, "/v1/reports/daily", app.requireAuth(app.dailyReportHandler))
	// The printable version of the same report. Named alongside the JSON
	// endpoint rather than nested under it, so the ".pdf" suffix stays part of
	// the path instead of colliding with the wildcard-free route above.
	router.HandlerFunc(http.MethodGet, "/v1/reports/daily.pdf", app.requireAuth(app.dailyReportPDFHandler))
	// Weekly consumables report, JSON and printable. Readable by any
	// authenticated user, like the daily report.
	router.HandlerFunc(http.MethodGet, "/v1/reports/consumables/weekly", app.requireAuth(app.weeklyConsumablesReportHandler))
	router.HandlerFunc(http.MethodGet, "/v1/reports/consumables/weekly.pdf", app.requireAuth(app.weeklyConsumablesPDFHandler))

	// LCU testing — a failed door card reader is trialled for a week before it is
	// scrapped. /v1/lcu/today is what the frontend asks on load, and the
	// gate-exempt pair here is what the daily prompt needs to do its job.
	router.HandlerFunc(http.MethodGet, "/v1/lcu/today", app.requireAuth(app.lcuTodayHandler))
	router.HandlerFunc(http.MethodPost, "/v1/lcu/tests", app.requireAuth(app.recordLCUTestsHandler))
	// Units: any authenticated user may start one for themselves; reading and
	// answering is the owner's or a manager's, checked per unit in the handlers.
	router.HandlerFunc(http.MethodGet, "/v1/lcu/units", app.requireAuth(app.listLCUUnitsHandler))
	router.HandlerFunc(http.MethodPost, "/v1/lcu/units", app.requireAuth(app.createLCUUnitHandler))
	router.HandlerFunc(http.MethodGet, "/v1/lcu/units/:id", app.requireAuth(app.getLCUUnitHandler))
	router.HandlerFunc(http.MethodDelete, "/v1/lcu/units/:id", app.requireAuth(app.deleteLCUUnitHandler))

	// lcuGate sits inside authenticate because it needs the resolved user, and
	// outside the router so it covers every route rather than the ones someone
	// remembered to wrap.
	return app.recoverPanic(app.enableCORS(app.rateLimit(app.authenticate(app.lcuGate(router)))))
}

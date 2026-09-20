package main

import (
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
)

// consumableItemForType maps an issue type to the inventory item tracked
// against it. Only these three types currently trigger the toggle in the
// issue form; anything else has no associated consumable.
var consumableItemForType = map[string]string{
	"door":   "AA Batteries",
	"remote": "Remote Control",
	"phone":  "Phone",
}

func consumableItemFor(issueType string) (string, bool) {
	item, ok := consumableItemForType[strings.ToLower(strings.TrimSpace(issueType))]
	return item, ok
}

// listConsumablesHandler powers the Consumables page: every tracked
// inventory transaction, optionally filtered to one date, plus a running
// total per item.
func (app *application) listConsumablesHandler(w http.ResponseWriter, r *http.Request) {
	date := app.readString(r.URL.Query(), "date", "")

	items, err := app.models.Consumables.GetAll(date)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if items == nil {
		items = []*data.Consumable{}
	}

	summary, err := app.models.Consumables.SummaryByItem()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"consumables": items,
		"summary":     summary,
	}, nil)
}

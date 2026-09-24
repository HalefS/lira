package main

import (
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// listConsumablesHandler powers the Consumables page: every recorded use of a
// consumable, optionally filtered to one date, plus a running total per item.
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
	if summary == nil {
		summary = []*data.ConsumableTotal{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"consumables": items,
		"summary":     summary,
	}, nil)
}

// validateConsumableUses checks the consumables selection sent with an issue
// create/update request: its shape first, then that every item is really in
// the catalog. Problems are added to v; the returned error is only for
// unexpected (database) failures.
func (app *application) validateConsumableUses(v *validator.Validator, uses []data.ConsumableUse) error {
	shape := validator.New()
	data.ValidateConsumableUses(shape, uses)
	if !shape.Valid() {
		for key, msg := range shape.Errors {
			v.AddError(key, msg)
		}
		return nil
	}
	if len(uses) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(uses))
	for _, u := range uses {
		ids = append(ids, u.ItemID)
	}
	found, err := app.models.ConsumableItems.CountByIDs(ids)
	if err != nil {
		return err
	}
	if found != len(ids) {
		v.AddError("consumables", "contains a consumable that does not exist")
	}
	return nil
}

package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listConsumableItemsHandler(w http.ResponseWriter, r *http.Request) {
	limit, capped := app.readLimit(r)

	items, total, err := app.models.ConsumableItems.GetAll(limit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if items == nil {
		items = []*data.ConsumableItem{}
	}
	app.writeJSON(w, http.StatusOK, envelope{
		"consumable_items": items,
		"total":            total,
		"limit":            limit,
		"limit_capped":     capped,
	}, nil)
}

func (app *application) createConsumableItemHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
		Icon string `json:"icon"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	icon := strings.TrimSpace(input.Icon)
	if icon == "" {
		icon = data.DefaultConsumableIcon
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	item := &data.ConsumableItem{
		Name:      strings.TrimSpace(input.Name),
		Icon:      icon,
		CreatedBy: &createdBy,
	}

	v := validator.New()
	if data.ValidateConsumableItem(v, item); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.ConsumableItems.Insert(item); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateConsumableItem):
			v.AddError("name", "a consumable with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"consumable_item": item}, nil)
}

// updateConsumableItemIconHandler lets a manager change the icon of an
// existing consumable, picked from the icon grid on the admin panel.
func (app *application) updateConsumableItemIconHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		Icon string `json:"icon"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	data.ValidateConsumableIcon(v, "icon", input.Icon)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	item, err := app.models.ConsumableItems.UpdateIcon(id, input.Icon)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"consumable_item": item}, nil)
}

// maxConsumableStockMovement bounds a single manual adjustment, so a typo
// cannot wipe out a real count.
const maxConsumableStockMovement = 100000

// adjustConsumableItemStockHandler lets a manager put stock in, take it out, or
// correct it to a counted figure. Every change is recorded in the item's
// movement history.
func (app *application) adjustConsumableItemStockHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		Change *int   `json:"change"`
		SetTo  *int   `json:"set_to"`
		Note   string `json:"note"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	switch {
	case input.Change == nil && input.SetTo == nil:
		v.AddError("stock", "provide either change (to add or remove) or set_to (to correct to a counted figure)")
	case input.Change != nil && input.SetTo != nil:
		v.AddError("stock", "provide either change or set_to, not both")
	case input.Change != nil:
		v.Check(*input.Change != 0, "change", "must not be zero")
		v.Check(*input.Change > -maxConsumableStockMovement && *input.Change < maxConsumableStockMovement,
			"change", "must be between -100000 and 100000")
	}
	v.Check(len(input.Note) <= 200, "note", "must not be more than 200 characters")
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	currentUser := app.contextGetUser(r)
	createdBy := currentUser.ID
	item, err := app.models.ConsumableItems.AdjustStock(id, input.Change, input.SetTo, strings.TrimSpace(input.Note), &createdBy)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		case strings.Contains(err.Error(), "would not change"),
			strings.Contains(err.Error(), "exactly one of"):
			// Already checked above; this only fires if the two disagree.
			app.failedValidationResponse(w, r, map[string]string{"stock": err.Error()})
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"consumable_item": item}, nil)
}

// updateConsumableItemReorderLevelHandler sets the count at or below which an
// item is flagged as running low.
func (app *application) updateConsumableItemReorderLevelHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		ReorderLevel int `json:"reorder_level"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	v.Check(input.ReorderLevel >= 0, "reorder_level", "must be zero or greater")
	v.Check(input.ReorderLevel <= 100000, "reorder_level", "must not be more than 100000")
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	item, err := app.models.ConsumableItems.UpdateReorderLevel(id, input.ReorderLevel)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"consumable_item": item}, nil)
}

// listStockMovementsHandler returns the inventory history, newest first.
// Read-only for every authenticated user: the movements are already visible on
// the Consumables page, and this endpoint is what the page's history view and
// any future export use.
func (app *application) listStockMovementsHandler(w http.ResponseWriter, r *http.Request) {
	itemID := int64(app.readInt(r.URL.Query(), "item_id", 0))
	limit := app.readInt(r.URL.Query(), "limit", 200)
	if limit < 1 || limit > 1000 {
		limit = 200
	}

	movements, err := app.models.ConsumableItems.StockMovements(itemID, limit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"movements": movements}, nil)
}

func (app *application) deleteConsumableItemHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.ConsumableItems.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "consumable successfully deleted"}, nil)
}

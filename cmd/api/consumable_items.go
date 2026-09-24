package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listConsumableItemsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := app.models.ConsumableItems.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if items == nil {
		items = []*data.ConsumableItem{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"consumable_items": items}, nil)
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

package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listIssuePresetsHandler(w http.ResponseWriter, r *http.Request) {
	presets, err := app.models.IssuePresets.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if presets == nil {
		presets = []*data.IssuePreset{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"issue_presets": presets}, nil)
}

func (app *application) readIssuePresetInput(w http.ResponseWriter, r *http.Request, input *struct {
	IssueTypeID int64                `json:"issue_type_id"`
	Name        string               `json:"name"`
	Problem     string               `json:"problem"`
	Resolution  string               `json:"resolution"`
	Consumables []data.ConsumableUse `json:"consumables"`
}) bool {
	if err := app.readJSON(w, r, input); err != nil {
		app.badRequestResponse(w, r, err)
		return false
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Problem = strings.TrimSpace(input.Problem)
	input.Resolution = strings.TrimSpace(input.Resolution)
	if input.Consumables == nil {
		input.Consumables = []data.ConsumableUse{}
	}
	return true
}

func (app *application) validateIssuePresetConsumables(v *validator.Validator, uses []data.ConsumableUse) error {
	if err := app.validateConsumableUses(v, uses); err != nil {
		return err
	}
	return nil
}

func (app *application) createIssuePresetHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IssueTypeID int64                `json:"issue_type_id"`
		Name        string               `json:"name"`
		Problem     string               `json:"problem"`
		Resolution  string               `json:"resolution"`
		Consumables []data.ConsumableUse `json:"consumables"`
	}
	if !app.readIssuePresetInput(w, r, &input) {
		return
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	preset := &data.IssuePreset{
		IssueTypeID: input.IssueTypeID,
		Name:        input.Name,
		Problem:     input.Problem,
		Resolution:  input.Resolution,
		CreatedBy:   &createdBy,
	}
	preset.Consumables = make([]data.IssuePresetConsumable, 0, len(input.Consumables))
	for _, u := range input.Consumables {
		preset.Consumables = append(preset.Consumables, data.IssuePresetConsumable{ItemID: u.ItemID, Quantity: u.Quantity})
	}

	v := validator.New()
	if data.ValidateIssuePreset(v, preset); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}
	if err := app.validateIssuePresetConsumables(v, input.Consumables); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.IssuePresets.Insert(preset); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateIssuePreset):
			v.AddError("name", "an issue preset with this name already exists for this issue type")
			app.failedValidationResponse(w, r, v.Errors)
		case errors.Is(err, data.ErrRecordNotFound):
			v.AddError("issue_type_id", "must reference a valid issue type")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	full, err := app.models.IssuePresets.Get(preset.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"issue_preset": full}, nil)
}

func (app *application) updateIssuePresetHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		IssueTypeID int64                `json:"issue_type_id"`
		Name        string               `json:"name"`
		Problem     string               `json:"problem"`
		Resolution  string               `json:"resolution"`
		Consumables []data.ConsumableUse `json:"consumables"`
	}
	if !app.readIssuePresetInput(w, r, &input) {
		return
	}

	preset := &data.IssuePreset{
		ID:          id,
		IssueTypeID: input.IssueTypeID,
		Name:        input.Name,
		Problem:     input.Problem,
		Resolution:  input.Resolution,
	}
	preset.Consumables = make([]data.IssuePresetConsumable, 0, len(input.Consumables))
	for _, u := range input.Consumables {
		preset.Consumables = append(preset.Consumables, data.IssuePresetConsumable{ItemID: u.ItemID, Quantity: u.Quantity})
	}

	v := validator.New()
	if data.ValidateIssuePreset(v, preset); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}
	if err := app.validateIssuePresetConsumables(v, input.Consumables); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.IssuePresets.Update(preset); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		case errors.Is(err, data.ErrDuplicateIssuePreset):
			v.AddError("name", "an issue preset with this name already exists for this issue type")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	full, err := app.models.IssuePresets.Get(preset.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"issue_preset": full}, nil)
}

func (app *application) deleteIssuePresetHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.IssuePresets.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "issue preset successfully deleted"}, nil)
}

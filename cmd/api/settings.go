package main

import (
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) getSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"settings": settings}, nil)
}

func (app *application) updateSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DuplicateWindowHours int `json:"duplicate_window_hours"`
		LCUWindowDays        int `json:"lcu_window_days"`
		// A pointer so an absent sla_minutes clears the SLA rather than silently
		// defaulting to zero, which would flag every issue that took any time.
		SLAMinutes *int `json:"sla_minutes"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	s := &data.Settings{
		DuplicateWindowHours: input.DuplicateWindowHours,
		LCUWindowDays:        input.LCUWindowDays,
		SLAMinutes:           input.SLAMinutes,
	}
	v := validator.New()
	if data.ValidateSettings(v, s); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	updated, err := app.models.Settings.Update(s.DuplicateWindowHours, s.LCUWindowDays, s.SLAMinutes, user.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"settings": updated}, nil)
}

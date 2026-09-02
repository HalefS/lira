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
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	s := &data.Settings{DuplicateWindowHours: input.DuplicateWindowHours}
	v := validator.New()
	if data.ValidateSettings(v, s); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	updated, err := app.models.Settings.Update(s.DuplicateWindowHours, user.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"settings": updated}, nil)
}

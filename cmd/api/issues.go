package main

import (
	"errors"
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listIssuesHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()

	filters := data.IssueFilters{
		Mode:   app.readString(qs, "mode", ""),
		Status: app.readString(qs, "status", ""),
		Type:   app.readString(qs, "type", ""),
		Search: app.readString(qs, "search", ""),
		Date:   app.readString(qs, "date", ""),
	}

	issues, err := app.models.Issues.GetAll(filters)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"issues": issues}, nil)
}

func (app *application) createIssueHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mode        string `json:"mode"`
		Location    string `json:"location"`
		Type        string `json:"type"`
		Problem     string `json:"problem"`
		Resolution  string `json:"resolution"`
		TimeMinutes int    `json:"time_minutes"`
		Status      string `json:"status"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)

	issue := &data.Issue{
		Mode:        input.Mode,
		Location:    input.Location,
		Type:        input.Type,
		Problem:     input.Problem,
		Resolution:  input.Resolution,
		TimeMinutes: input.TimeMinutes,
		Status:      input.Status,
		LoggedBy:    user.ID,
	}

	if issue.Status == "" {
		issue.Status = "Pending"
	}

	v := validator.New()
	if data.ValidateIssue(v, issue); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Issues.Insert(issue); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// return issue with user info populated
	fullIssue, err := app.models.Issues.Get(issue.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"issue": fullIssue}, nil)
}

func (app *application) showIssueHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"issue": issue}, nil)
}

func (app *application) updateIssueHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Permission: only the owner or a manager can edit
	currentUser := app.contextGetUser(r)
	if currentUser.Role != "manager" && issue.LoggedBy != currentUser.ID {
		app.notPermittedResponse(w, r)
		return
	}

	var input struct {
		Mode        *string `json:"mode"`
		Location    *string `json:"location"`
		Type        *string `json:"type"`
		Problem     *string `json:"problem"`
		Resolution  *string `json:"resolution"`
		TimeMinutes *int    `json:"time_minutes"`
		Status      *string `json:"status"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	if input.Mode != nil {
		issue.Mode = *input.Mode
	}
	if input.Location != nil {
		issue.Location = *input.Location
	}
	if input.Type != nil {
		issue.Type = *input.Type
	}
	if input.Problem != nil {
		issue.Problem = *input.Problem
	}
	if input.Resolution != nil {
		issue.Resolution = *input.Resolution
	}
	if input.TimeMinutes != nil {
		issue.TimeMinutes = *input.TimeMinutes
	}
	if input.Status != nil {
		issue.Status = *input.Status
	}

	v := validator.New()
	if data.ValidateIssue(v, issue); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Issues.Update(issue); err != nil {
		switch {
		case errors.Is(err, data.ErrEditConflict):
			app.editConflictResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Return with user info
	fullIssue, err := app.models.Issues.Get(issue.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"issue": fullIssue}, nil)
}

func (app *application) deleteIssueHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	issue, err := app.models.Issues.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Permission: only owner or manager can delete
	currentUser := app.contextGetUser(r)
	if currentUser.Role != "manager" && issue.LoggedBy != currentUser.ID {
		app.notPermittedResponse(w, r)
		return
	}

	if err := app.models.Issues.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "issue successfully deleted"}, nil)
}

func (app *application) statsHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	date := app.readString(qs, "date", "")

	stats, err := app.models.Issues.GetStats(date)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"stats": stats}, nil)
}

func (app *application) healthcheckHandler(w http.ResponseWriter, r *http.Request) {
	env := envelope{
		"status": "available",
		"system_info": map[string]string{
			"environment": app.config.env,
			"version":     version,
		},
	}
	app.writeJSON(w, http.StatusOK, env, nil)
}

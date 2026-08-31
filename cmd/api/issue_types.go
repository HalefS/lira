package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listIssueTypesHandler(w http.ResponseWriter, r *http.Request) {
	types, err := app.models.IssueTypes.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if types == nil {
		types = []*data.IssueType{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"issue_types": types}, nil)
}

func (app *application) createIssueTypeHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	it := &data.IssueType{
		Name:      strings.TrimSpace(input.Name),
		CreatedBy: &createdBy,
	}

	v := validator.New()
	if data.ValidateIssueType(v, it); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.IssueTypes.Insert(it); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateIssueType):
			v.AddError("name", "an issue type with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"issue_type": it}, nil)
}

func (app *application) deleteIssueTypeHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.IssueTypes.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "issue type successfully deleted"}, nil)
}

// resolveIssueType returns the canonical catalog name. If the name is not in
// the catalog, allowCurrent lets an existing issue keep its stored type.
func (app *application) resolveIssueType(name, allowCurrent string) (string, error) {
	it, err := app.models.IssueTypes.GetByName(name)
	if err == nil {
		return it.Name, nil
	}
	if !errors.Is(err, data.ErrRecordNotFound) {
		return "", err
	}
	if allowCurrent != "" && strings.EqualFold(strings.TrimSpace(name), allowCurrent) {
		return allowCurrent, nil
	}
	return "", data.ErrRecordNotFound
}

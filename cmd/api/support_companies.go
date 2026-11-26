package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// The third-party support catalog. Reading is open to every signed-in user --
// the issue form needs the list to offer companies -- while adding, renaming,
// recolouring and deactivating are manager actions.
func (app *application) listSupportCompaniesHandler(w http.ResponseWriter, r *http.Request) {
	companies, err := app.models.SupportCompanies.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if companies == nil {
		companies = []*data.SupportCompany{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"support_companies": companies}, nil)
}

func (app *application) createSupportCompanyHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	c := &data.SupportCompany{
		Name:      strings.TrimSpace(input.Name),
		Color:     input.Color,
		FieldKind: data.SupportFieldReference,
		Active:    true,
		CreatedBy: &createdBy,
	}
	if c.Color == "" {
		c.Color = "blue"
	}

	v := validator.New()
	data.ValidateSupportCompany(v, c)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.SupportCompanies.Insert(c); err != nil {
		if errors.Is(err, data.ErrDuplicateSupportCompany) {
			v.AddError("name", "a company with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"support_company": c}, nil)
}

// updateSupportCompanyHandler renames, recolours or deactivates a company.
//
// Every field is optional so a caller can change one without sending the rest;
// the row is read first and the supplied fields are applied to it. The slug is
// never touched: it is what existing handovers reference.
func (app *application) updateSupportCompanyHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		Name   *string `json:"name"`
		Color  *string `json:"color"`
		Active *bool   `json:"active"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	c, err := app.models.SupportCompanies.Get(id)
	if errors.Is(err, data.ErrRecordNotFound) {
		app.notFoundResponse(w, r)
		return
	}
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if input.Name != nil {
		c.Name = strings.TrimSpace(*input.Name)
	}
	if input.Color != nil {
		c.Color = *input.Color
	}
	if input.Active != nil {
		c.Active = *input.Active
	}

	v := validator.New()
	data.ValidateSupportCompany(v, c)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.SupportCompanies.Update(c); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		case errors.Is(err, data.ErrDuplicateSupportCompany):
			v.AddError("name", "a company with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"support_company": c}, nil)
}

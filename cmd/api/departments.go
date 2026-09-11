package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listDepartmentsHandler(w http.ResponseWriter, r *http.Request) {
	depts, err := app.models.Departments.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if depts == nil {
		depts = []*data.Department{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"departments": depts}, nil)
}

func (app *application) createDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	d := &data.Department{
		Name:      strings.TrimSpace(input.Name),
		CreatedBy: &createdBy,
	}

	v := validator.New()
	if data.ValidateDepartment(v, d); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Departments.Insert(d); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateDepartment):
			v.AddError("name", "a department with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"department": d}, nil)
}

func (app *application) deleteDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.Departments.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "department successfully deleted"}, nil)
}

// resolveDepartment returns the canonical catalog name for a department. If
// the name isn't in the catalog, allowCurrent lets an existing issue keep
// its already-stored department name (e.g. one later renamed or removed
// from the catalog).
func (app *application) resolveDepartment(name, allowCurrent string) (string, error) {
	d, err := app.models.Departments.GetByName(name)
	if err == nil {
		return d.Name, nil
	}
	if !errors.Is(err, data.ErrRecordNotFound) {
		return "", err
	}
	if allowCurrent != "" && strings.EqualFold(strings.TrimSpace(name), allowCurrent) {
		return allowCurrent, nil
	}
	return "", data.ErrRecordNotFound
}

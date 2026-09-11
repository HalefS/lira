package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) listConnectaAgentsHandler(w http.ResponseWriter, r *http.Request) {
	agents, err := app.models.ConnectaAgents.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if agents == nil {
		agents = []*data.ConnectaAgent{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"connecta_agents": agents}, nil)
}

func (app *application) createConnectaAgentHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	a := &data.ConnectaAgent{
		Name:      strings.TrimSpace(input.Name),
		CreatedBy: &createdBy,
	}

	v := validator.New()
	if data.ValidateConnectaAgent(v, a); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.ConnectaAgents.Insert(a); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateConnectaAgent):
			v.AddError("name", "an agent with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"connecta_agent": a}, nil)
}

func (app *application) deleteConnectaAgentHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.ConnectaAgents.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "connecta agent successfully deleted"}, nil)
}

// resolveConnectaAgent returns the canonical catalog name for an agent. If
// the name isn't in the catalog, allowCurrent lets an existing issue keep
// whatever agent name it already had stored (e.g. one later renamed or
// removed from the catalog).
func (app *application) resolveConnectaAgent(name, allowCurrent string) (string, error) {
	a, err := app.models.ConnectaAgents.GetByName(name)
	if err == nil {
		return a.Name, nil
	}
	if !errors.Is(err, data.ErrRecordNotFound) {
		return "", err
	}
	if allowCurrent != "" && strings.EqualFold(strings.TrimSpace(name), allowCurrent) {
		return allowCurrent, nil
	}
	return "", data.ErrRecordNotFound
}

// resolveOptionalAgentField normalizes and validates an optional Connecta
// Agent field: nil or blank means "not set" and resolves to nil; otherwise
// the name must exist in the catalog (or match allowCurrent, so editing an
// issue doesn't break if that catalog entry was later renamed or removed).
func (app *application) resolveOptionalAgentField(v *validator.Validator, field string, name *string, allowCurrent string) (*string, error) {
	if name == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*name)
	if trimmed == "" {
		return nil, nil
	}
	canonical, err := app.resolveConnectaAgent(trimmed, allowCurrent)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			v.AddError(field, "must be a valid Connecta Agent")
			return name, nil
		}
		return nil, err
	}
	return &canonical, nil
}

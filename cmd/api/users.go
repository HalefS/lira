package main

import (
	"errors"
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// POST /v1/users
func (app *application) registerUserHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Language string `json:"language"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := &data.User{
		Name:     input.Name,
		Email:    input.Email,
		// Self-registration always creates a technician account.
		// Promotion to manager can only be done by an existing manager
		// via PATCH /v1/users/:id/role.
		Role:     "technician",
		Language: input.Language,
	}
	if user.Language == "" {
		user.Language = "en"
	}

	if err := user.Password.Set(input.Password); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	v := validator.New()
	if data.ValidateUser(v, user); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	count, err := app.models.Users.Count()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	user.AvatarIdx = int(count % 5)

	if err := app.models.Users.Insert(user); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateEmail):
			v.AddError("email", "a user with this email address already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	token, err := app.models.Tokens.New(user.ID, 24*7, data.ScopeAuthentication)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"user": user, "token": token.Plaintext}, nil)
}

// GET /v1/users
func (app *application) listUsersHandler(w http.ResponseWriter, r *http.Request) {
	users, err := app.models.Users.GetAll()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"users": users}, nil)
}

// GET /v1/users/:id
func (app *application) getUserHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}
	user, err := app.models.Users.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"user": user}, nil)
}

// GET /v1/users/me
func (app *application) getMeHandler(w http.ResponseWriter, r *http.Request) {
	user := app.contextGetUser(r)
	// Fetch fresh from DB to get latest fields
	fresh, err := app.models.Users.Get(user.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"user": fresh}, nil)
}

// PATCH /v1/users/me
func (app *application) updateMeHandler(w http.ResponseWriter, r *http.Request) {
	currentUser := app.contextGetUser(r)

	user, err := app.models.Users.Get(currentUser.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	var input struct {
		Name       *string `json:"name"`
		Email      *string `json:"email"`
		Password   *string `json:"password"`
		Language   *string `json:"language"`
		AvatarData *string `json:"avatar_data"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	if input.Name != nil {
		user.Name = *input.Name
	}
	if input.Email != nil {
		user.Email = *input.Email
	}
	if input.Language != nil {
		user.Language = *input.Language
	}
	if input.AvatarData != nil {
		user.AvatarData = *input.AvatarData
	}
	if input.Password != nil {
		if err := user.Password.Set(*input.Password); err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	v := validator.New()
	v.Check(user.Name != "", "name", "must be provided")
	validator.ValidateEmail(v, user.Email)
	if input.Password != nil {
		validator.ValidatePasswordPlaintext(v, *input.Password)
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Users.Update(user); err != nil {
		switch {
		case errors.Is(err, data.ErrDuplicateEmail):
			v.AddError("email", "a user with this email address already exists")
			app.failedValidationResponse(w, r, v.Errors)
		case errors.Is(err, data.ErrEditConflict):
			app.editConflictResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"user": user}, nil)
}

// GET /v1/users/me/stats
func (app *application) getMeStatsHandler(w http.ResponseWriter, r *http.Request) {
	user := app.contextGetUser(r)
	stats, err := app.models.Issues.GetUserStats(user.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"stats": stats}, nil)
}

// GET /v1/users/me/issues
func (app *application) getMeIssuesHandler(w http.ResponseWriter, r *http.Request) {
	user := app.contextGetUser(r)
	limit := app.readInt(r.URL.Query(), "limit", 10)
	issues, err := app.models.Issues.GetByUser(user.ID, limit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"issues": issues}, nil)
}

// PATCH /v1/users/:id/role  (manager only)
func (app *application) updateUserRoleHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	user, err := app.models.Users.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	var input struct {
		Role string `json:"role"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	v.Check(input.Role == "technician" || input.Role == "manager", "role", "must be 'technician' or 'manager'")
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user.Role = input.Role
	if err := app.models.Users.Update(user); err != nil {
		switch {
		case errors.Is(err, data.ErrEditConflict):
			app.editConflictResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"user": user}, nil)
}

// PATCH /v1/users/:id/deactivate  (manager only)
func (app *application) deactivateUserHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	currentUser := app.contextGetUser(r)
	if id == currentUser.ID {
		app.errorResponse(w, r, http.StatusBadRequest, "you cannot deactivate your own account")
		return
	}

	user, err := app.models.Users.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	var input struct {
		Active bool `json:"active"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user.Active = input.Active

	// Revoke all tokens if deactivating
	if !user.Active {
		app.models.Tokens.DeleteAllForUser(data.ScopeAuthentication, user.ID)
	}

	if err := app.models.Users.Update(user); err != nil {
		switch {
		case errors.Is(err, data.ErrEditConflict):
			app.editConflictResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"user": user}, nil)
}

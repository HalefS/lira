package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

func (app *application) createAuthTokenHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	validator.ValidateEmail(v, input.Email)
	// Only check that a password was supplied here — the length/strength
	// rules in ValidatePasswordPlaintext apply at registration time, not
	// login, since some accounts (e.g. the seeded default) may predate
	// or bypass those rules.
	v.Check(input.Password != "", "password", "must be provided")
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user, err := app.models.Users.GetByEmail(input.Email)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.invalidCredentialsResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	match, err := user.Password.Matches(input.Password)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if !match {
		app.invalidCredentialsResponse(w, r)
		return
	}

	// Delete any existing auth tokens for this user (single-session)
	app.models.Tokens.DeleteAllForUser(data.ScopeAuthentication, user.ID)

	token, err := app.models.Tokens.New(user.ID, 24*7*time.Hour, data.ScopeAuthentication)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{
		"token": token.Plaintext,
		"user":  user,
	}, nil)
}

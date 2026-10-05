package main

import (
	"errors"
	"net/http"
	"strings"
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

	// A manager asked this account to choose a new password. The password was
	// right, but the sign-in still does not complete: no token is issued, so
	// there is no window in which the old password is good for anything. The
	// flag tells the client to show the "choose a new password" step instead of
	// treating this as a failure.
	if user.MustResetPassword {
		app.writeJSON(w, http.StatusForbidden, envelope{
			"error":               "a new password is required before you can sign in",
			"must_reset_password": true,
		}, nil)
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

// deleteAuthTokenHandler signs the caller out.
//
// Until this existed, signing out only forgot the token in the browser: the row
// stayed in the database and the token stayed good for the full seven days. On a
// shared machine that is the machine in a hotel reception, and it is also what
// would make an inactivity timeout decorative -- the session would end on screen
// and carry on being usable.
//
// Idempotent, and deliberately quiet about what it removed: a caller whose token
// has already expired, or who signs out twice, gets the same answer as one who
// signs out once. There is nothing for them to do differently either way.
func (app *application) deleteAuthTokenHandler(w http.ResponseWriter, r *http.Request) {
	headerParts := strings.Split(r.Header.Get("Authorization"), " ")
	if len(headerParts) == 2 && headerParts[0] == "Bearer" {
		if err := app.models.Tokens.DeleteByPlaintext(data.ScopeAuthentication, headerParts[1]); err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	// 204 rather than 200 with a body: there is nothing to report, and a body
	// would only invite a client to look for a field in it.
	w.WriteHeader(http.StatusNoContent)
}

package main

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// A password change in this system is started by a manager, not by the user.
//
// The manager does not pick or send a password. They raise a flag that stops
// the account getting a session, and every existing session is revoked at the
// same moment. The user then signs in with the password they already have, is
// told to choose a new one, and only then gets a session. That is the whole
// flow: no code, no email, and nothing invented on the user's behalf.

// passwordChangeWindow is how long one address must wait between attempts at
// the forced change. The endpoint is unauthenticated and checks a password, so
// without it this would be an unthrottled oracle for guessing one account.
const passwordChangeWindow = 6 * time.Second

// passwordChangeThrottled is the rate-limit body.
const passwordChangeThrottled = "Too many attempts. Please wait a moment and try again."

// POST /v1/users/:id/reset-password  (manager only)
// {"required": true}
//
// Tells the account's owner to choose a new password, and signs them out
// everywhere immediately. required=false takes the request back, which is what
// the same button does when a reset is already pending. Returns the user.
func (app *application) resetUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	// Defaults to true when the field is absent, so the endpoint is safe to call
	// with no body at all: the destructive direction is never the one that has to
	// be spelled out.
	var input struct {
		Required *bool `json:"required"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}
	required := true
	if input.Required != nil {
		required = *input.Required
	}

	currentUser := app.contextGetUser(r)

	user, err := app.models.Users.SetPasswordResetRequired(id, required)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Revoked after the flag is set, not before: a token minted in between the
	// two would be rejected by GetForToken anyway, but leaving the rows behind
	// would let the user keep using one they already hold until it expires.
	if err := app.models.Tokens.DeleteAllForUser(data.ScopeAuthentication, user.ID); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.logger.Info("manager set the password change requirement",
		"target_user_id", user.ID, "required", required,
		"by_user_id", currentUser.ID, "by_user_email", currentUser.Email)

	app.writeJSON(w, http.StatusOK, envelope{"user": user}, nil)
}

// POST /v1/password-change
// {"email": "...", "current_password": "...", "new_password": "..."}
//
// The user's half of the flow. Outside authenticate on purpose: the whole point
// is that they have no session.
//
// The current password is required, not because the manager knows it, but
// because it is the only proof of identity available. A manager-triggered reset
// creates nothing and sends nothing, so the account's existing password is the
// only thing tying the person at the keyboard to the account. On success it
// signs them in, since they have just proved who they are.
func (app *application) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	email := input.Email

	v := validator.New()
	validator.ValidateEmail(v, email)
	v.Check(input.CurrentPassword != "", "current_password", "must be provided")
	// Checked before hashing: there is no point spending bcrypt on a password
	// that is going to be rejected anyway.
	validator.ValidatePasswordPlaintext(v, input.NewPassword)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// Throttled per address, charged only once the request is otherwise valid,
	// so a malformed field cannot lock a user out of their own change.
	if allowed, wait := app.passwordChangeThrottle.Allow(email, passwordChangeWindow); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		app.writeJSON(w, http.StatusTooManyRequests, envelope{"error": passwordChangeThrottled}, nil)
		return
	}

	user, err := app.models.Users.GetByEmail(email)
	if err != nil || !user.Active {
		// One answer for an unknown address and a deactivated account, and the
		// same answer as a wrong password below. This endpoint must not become
		// a way to find out who has an account here.
		app.invalidCredentialsResponse(w, r)
		return
	}

	match, err := user.Password.Matches(input.CurrentPassword)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if !match {
		app.invalidCredentialsResponse(w, r)
		return
	}

	// Refuse when no reset is pending, so this cannot be used as a second way to
	// change a password and sidestep the session check on ordinary accounts.
	if !user.MustResetPassword {
		app.writeJSON(w, http.StatusBadRequest, envelope{
			"error": "no password change is pending for this account",
		}, nil)
		return
	}

	pw, err := data.NewPasswordFrom(input.NewPassword)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	updated, err := app.models.Users.SetPasswordAndClearReset(user.ID, pw.Hash())
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Any token that survived the manager's reset is now worthless: the password
	// it was issued against has changed. Cleared before the new one is minted so
	// the single-session rule still holds.
	if err := app.models.Tokens.DeleteAllForUser(data.ScopeAuthentication, user.ID); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	token, err := app.models.Tokens.New(updated.ID, 24*7*time.Hour, data.ScopeAuthentication)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.logger.Info("password changed after a manager request", "user_id", updated.ID)

	app.writeJSON(w, http.StatusCreated, envelope{
		"token": token.Plaintext,
		"user":  updated,
	}, nil)
}

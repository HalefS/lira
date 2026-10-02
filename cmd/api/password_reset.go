package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// A password change is started by a manager and finished by the user.
//
// A manager presses the button and gets a six-digit code. They read it out to
// the person who forgot their password — over a phone, in person, on a piece of
// paper — and that code is what lets the user choose a new password. Nothing is
// emailed and nobody has to remember an old password, which is the point: this
// is the recovery path for someone who cannot remember their password at all.
//
// The code is only ever returned here, in plaintext, once. The server keeps a
// bcrypt hash and nothing else, so it cannot be recovered or looked up later.
// Generating another code replaces the first.

// passwordChangeWindow is how long one address must wait between attempts at
// redeeming a code. The endpoint is unauthenticated and the code is six digits,
// so without this it would be an unthrottled oracle for guessing one account.
const passwordChangeWindow = 6 * time.Second

// passwordChangeThrottled is the rate-limit body.
const passwordChangeThrottled = "Too many attempts. Please wait a moment and try again."

// POST /v1/users/:id/reset-password  (manager only)
// {"required": true}
//
// Issues a reset code, or clears a pending one when required is false, for a
// manager who pressed the button on the wrong person.
//
// Returns the code. This is the only time it exists outside the manager who
// asked for it.
func (app *application) resetUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	// Defaults to true when the field is absent, so the endpoint is safe to call
	// with no body at all: the state-changing direction is never the one that has
	// to be spelled out.
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

	// Clearing first, on the way out of a reset. A user who cannot get in has
	// no use for the flag, and leaving it set after a code has been redeemed
	// would bar them from signing in for good.
	var user *data.User
	var code string
	if required {
		code, user, err = app.models.Users.IssueResetCode(id, data.ResetCodeTTL)
	} else {
		user, err = app.models.Users.ClearPasswordReset(id)
	}
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Revoked after the code is issued, not before: a token minted in between the
	// two would be rejected by GetForToken anyway, but leaving the rows behind
	// would let the user keep using one they already hold until it expires.
	if err := app.models.Tokens.DeleteAllForUser(data.ScopeAuthentication, user.ID); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.logger.Info("manager set the password reset",
		"target_user_id", user.ID, "required", required,
		"by_user_id", currentUser.ID, "by_user_email", currentUser.Email)

	response := envelope{
		"user":      user,
		"expires_in": int(data.ResetCodeTTL.Seconds()),
	}
	if required {
		// Present only on this response. The hash is all the server keeps.
		response["code"] = code
	}
	app.writeJSON(w, http.StatusOK, response, nil)
}

// POST /v1/password-change
// {"email": "...", "code": "123456", "new_password": "..."}
//
// The user's half of the flow. Outside authenticate on purpose: the account has
// been signed out of every session and has no token to present.
//
// On success it signs them in, since a valid code is proof enough — there is no
// old password left to prove anything with.
func (app *application) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	email := strings.TrimSpace(input.Email)
	code := strings.TrimSpace(input.Code)

	v := validator.New()
	validator.ValidateEmail(v, email)
	// Checked rather than compared exactly, so a six-digit code is required
	// without also rejecting a padded or oddly-formatted one from a client.
	v.Check(code != "", "code", "must be provided")
	v.Check(len(code) <= 16, "code", "must be at most 16 characters")
	// The same rule as any other password in the app, so a reset cannot set
	// something weaker than signup allows. Checked before hashing: there is no
	// point spending bcrypt on a password that is going to be rejected.
	validator.ValidatePasswordPlaintext(v, input.NewPassword)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// Throttled per address, charged only once the request is otherwise valid,
	// so a malformed field cannot lock a user out of their own reset.
	if allowed, wait := app.passwordChangeThrottle.Allow(email, passwordChangeWindow); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		app.writeJSON(w, http.StatusTooManyRequests, envelope{"error": passwordChangeThrottled}, nil)
		return
	}

	pw, err := data.NewPasswordFrom(input.NewPassword)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	updated, err := app.models.Users.RedeemResetCode(email, code, pw.Hash())
	switch {
	case errors.Is(err, data.ErrInvalidResetCode):
		// One message for a wrong code, a used one, an expired one and an
		// account with no reset pending, so this cannot be used to find out who
		// has an account here or whose reset is outstanding.
		nv := validator.New()
		nv.AddError("code", "that code is not valid or has expired")
		app.failedValidationResponse(w, r, nv.Errors)
		return
	case err != nil:
		app.serverErrorResponse(w, r, err)
		return
	}

	// The old password is gone and the account was already signed out, so any
	// token left over is worthless. Cleared before the new one is minted so the
	// single-session rule still holds.
	if err := app.models.Tokens.DeleteAllForUser(data.ScopeAuthentication, updated.ID); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	token, err := app.models.Tokens.New(updated.ID, 24*7*time.Hour, data.ScopeAuthentication)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.logger.Info("password changed with a manager-issued code", "user_id", updated.ID)

	app.writeJSON(w, http.StatusCreated, envelope{
		"token": token.Plaintext,
		"user":  updated,
	}, nil)
}

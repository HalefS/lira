package main

import (
	"errors"

	"github.com/HalefS/lira/internal/data"
)

// defaultManagerEmail/defaultManagerPassword are the credentials for the
// built-in manager account that LIRA seeds automatically on first run, so
// there's always at least one manager able to log in and promote other
// users. The login system is email-based, so "lira" becomes this address;
// the account name and password stay literally "lira" as requested.
const (
	defaultManagerName     = "lira"
	defaultManagerEmail    = "lira@lira.local"
	defaultManagerPassword = "lira"
)

// seedDefaultManager ensures a default manager account exists so the
// application is usable out of the box. It's a no-op if that account (or
// any account with the same email) already exists. It never overwrites an
// existing account, so changing the seeded password later is safe.
func (app *application) seedDefaultManager() error {
	_, err := app.models.Users.GetByEmail(defaultManagerEmail)
	if err == nil {
		// Already seeded.
		return nil
	}
	if !errors.Is(err, data.ErrRecordNotFound) {
		return err
	}

	user := &data.User{
		Name:     defaultManagerName,
		Email:    defaultManagerEmail,
		Role:     "manager",
		Language: "en",
	}

	// Set directly rather than going through the public registration
	// validator: the default password is intentionally short ("lira"),
	// which wouldn't pass the normal sign-up strength check.
	if err := user.Password.Set(defaultManagerPassword); err != nil {
		return err
	}

	if err := app.models.Users.Insert(user); err != nil {
		if errors.Is(err, data.ErrDuplicateEmail) {
			// Created concurrently by another instance/process — fine.
			return nil
		}
		return err
	}

	app.logger.Info("seeded default manager account",
		"email", defaultManagerEmail, "password", defaultManagerPassword)
	return nil
}

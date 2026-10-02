package data

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ResetCodeTTL is how long a manager-issued code stays usable.
//
// Short on purpose. The code is six digits and travels by whatever means the
// two of them happen to use — read out over a phone, handed over on a piece of
// paper — so the window is kept to about one shift. A manager who misses the
// moment just generates another one, which is a single click.
const ResetCodeTTL = time.Hour

// resetCodeDigits is the length of the generated code. Six digits is what fits
// on a phone call without spelling it out letter by letter.
const resetCodeDigits = 6

// ErrInvalidResetCode is returned when a code does not match, has expired, has
// already been used, or was never issued. Every one of those cases returns the
// same error on purpose: telling them apart would let someone probe which
// accounts have a pending reset.
var ErrInvalidResetCode = errors.New("that reset code is not valid or has expired")

// GenerateResetCode returns a zero-padded six-digit code drawn from
// crypto/rand. Zero-padded so it reads as six digits however it is written down.
func GenerateResetCode() (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(resetCodeDigits), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("data: could not generate a reset code: %w", err)
	}
	return fmt.Sprintf("%0*d", resetCodeDigits, n.Int64()), nil
}

// hashResetCode is what goes in reset_code_hash.
//
// bcrypt rather than a plain digest: six digits is only a million candidates,
// so a fast hash would let anyone holding a database dump enumerate every
// possible code in seconds and defeat the whole mechanism. bcrypt is slow
// enough that it does not, and the only place it is verified is behind a
// per-address rate limit, so its cost is never felt.
func hashResetCode(code string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(code), 12)
}

// verifyResetCode reports whether code matches a stored hash.
func verifyResetCode(stored []byte, code string) bool {
	if len(stored) == 0 || code == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword(stored, []byte(code)) == nil
}

// IssueResetCode raises the pending flag, stores a fresh code, and returns the
// plaintext alongside the updated user.
//
// The plaintext is returned here and nowhere else, so it is only ever readable
// in the one response the manager sees. Generating a second code replaces the
// first: a manager who has lost the first one should not have to wonder whether
// the old one still works.
//
// It deliberately does not bump version, matching SetPasswordResetRequired: it
// is a targeted write that must not collide with a manager editing the same
// user's role or profile at the same moment.
func (m UserModel) IssueResetCode(id int64, ttl time.Duration) (string, *User, error) {
	code, err := GenerateResetCode()
	if err != nil {
		return "", nil, err
	}
	hash, err := hashResetCode(code)
	if err != nil {
		return "", nil, err
	}

	query := `
		UPDATE users
		SET must_reset_password = true, reset_code_hash = $1, reset_code_expires_at = $2
		WHERE id = $3
		RETURNING id, created_at, name, email, password_hash, role,
			avatar_idx, avatar_data, language, active, must_reset_password, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	u, err := scanUser(m.DB.QueryRowContext(ctx, query, hash, time.Now().Add(ttl), id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, ErrRecordNotFound
		}
		return "", nil, err
	}
	return code, u, nil
}

// ClearPasswordReset lowers the pending flag and destroys any code, for a
// manager who pressed the button on the wrong person.
func (m UserModel) ClearPasswordReset(id int64) (*User, error) {
	query := `
		UPDATE users
		SET must_reset_password = false, reset_code_hash = NULL, reset_code_expires_at = NULL
		WHERE id = $1
		RETURNING id, created_at, name, email, password_hash, role,
			avatar_idx, avatar_data, language, active, must_reset_password, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	u, err := scanUser(m.DB.QueryRowContext(ctx, query, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return u, nil
}

// RedeemResetCode checks a code against a pending reset and, if it is good,
// stores the new password and clears the reset in one statement.
//
// The code is verified in Go because it is a bcrypt hash and cannot be matched
// in SQL. The update that follows still re-checks that a reset is live
// (must_reset_password AND reset_code_hash IS NOT NULL), so two requests racing
// on the same code cannot both succeed: the second finds the column already
// nulled and gets ErrInvalidResetCode.
//
// version is bumped, unlike the manager-side writes, because a password change
// invalidates any full-row edit prepared from the old row.
func (m UserModel) RedeemResetCode(email, code string, newHash []byte) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Look the account up first. The address is citext, so this also matches
	// however the user typed the capitalisation.
	lookup := `SELECT id, reset_code_hash, reset_code_expires_at, must_reset_password
		FROM users WHERE email = $1 AND active = true`
	var id int64
	var stored []byte
	var expires sql.NullTime
	var pending bool

	err := m.DB.QueryRowContext(ctx, lookup, email).Scan(&id, &stored, &expires, &pending)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidResetCode
		}
		return nil, err
	}

	if !pending || !expires.Valid || time.Now().After(expires.Time) {
		return nil, ErrInvalidResetCode
	}
	if !verifyResetCode(stored, code) {
		return nil, ErrInvalidResetCode
	}

	update := `
		UPDATE users
		SET password_hash = $1, must_reset_password = false,
			reset_code_hash = NULL, reset_code_expires_at = NULL,
			version = version + 1
		WHERE id = $2 AND must_reset_password = true AND reset_code_hash IS NOT NULL
		RETURNING id, created_at, name, email, password_hash, role,
			avatar_idx, avatar_data, language, active, must_reset_password, version`

	u, err := scanUser(m.DB.QueryRowContext(ctx, update, newHash, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Another request redeemed the same code a moment ago.
			return nil, ErrInvalidResetCode
		}
		return nil, err
	}
	return u, nil
}

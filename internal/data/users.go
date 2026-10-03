package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"github.com/HalefS/lira/internal/validator"
	"golang.org/x/crypto/bcrypt"
)

var AnonymousUser = &User{}

type User struct {
	ID         int64     `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	Password   password  `json:"-"`
	Role       string    `json:"role"`
	AvatarIdx  int       `json:"avatar_idx"`
	AvatarData string    `json:"avatar_data"`
	Language   string    `json:"language"`
	Active     bool      `json:"active"`
	// MustResetPassword is set by a manager and makes the next sign-in stop
	// and ask for a new password before any session is issued.
	MustResetPassword bool `json:"must_reset_password"`
	Version           int  `json:"-"`
}

func (u *User) IsAnonymous() bool { return u == AnonymousUser }

type password struct {
	plaintext *string
	hash      []byte
}

func (p *password) Set(plaintextPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), 12)
	if err != nil {
		return err
	}
	p.plaintext = &plaintextPassword
	p.hash = hash
	return nil
}

// NewPassword is a password built from a plaintext string, for the paths that
// set one without going through a User struct -- the forced password change
// being the current example. It hashes immediately; the caller is expected to
// validate the plaintext separately with validator.ValidatePasswordPlaintext.
type NewPassword struct {
	password
}

// NewPasswordFrom builds a NewPassword from plaintext, or the hashing error.
func NewPasswordFrom(plaintext string) (*NewPassword, error) {
	var p NewPassword
	if err := p.Set(plaintext); err != nil {
		return nil, err
	}
	return &p, nil
}

// Hash returns the bcrypt hash, for the callers that store a password without
// carrying a whole User around -- the forced password change updates
// `users.password_hash` directly.
func (p *password) Hash() []byte { return p.hash }

func (p *password) Matches(plaintextPassword string) (bool, error) {
	err := bcrypt.CompareHashAndPassword(p.hash, []byte(plaintextPassword))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func ValidateUser(v *validator.Validator, user *User) {
	v.Check(user.Name != "", "name", "must be provided")
	v.Check(len(user.Name) <= 500, "name", "must not be more than 500 bytes long")
	validator.ValidateEmail(v, user.Email)
	v.Check(user.Role == "technician" || user.Role == "manager", "role", "must be 'technician' or 'manager'")
	if user.Password.plaintext != nil {
		validator.ValidatePasswordPlaintext(v, *user.Password.plaintext)
	}
	if user.Password.hash == nil {
		panic("missing password hash for user")
	}
}

type UserModel struct{ DB *sql.DB }

func (m UserModel) Insert(user *User) error {
	if user.Language == "" {
		user.Language = "en"
	}
	user.Active = true
	query := `
		INSERT INTO users (name, email, password_hash, role, avatar_idx, avatar_data, language, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, version`
	args := []any{user.Name, user.Email, user.Password.hash, user.Role,
		user.AvatarIdx, user.AvatarData, user.Language, user.Active}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&user.ID, &user.CreatedAt, &user.Version)
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "users_email_key"` {
			return ErrDuplicateEmail
		}
		return err
	}
	return nil
}

func (m UserModel) Get(id int64) (*User, error) {
	query := `SELECT id, created_at, name, email, password_hash, role,
		avatar_idx, avatar_data, language, active, must_reset_password, version FROM users WHERE id=$1`
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

func (m UserModel) GetByEmail(email string) (*User, error) {
	query := `SELECT id, created_at, name, email, password_hash, role,
		avatar_idx, avatar_data, language, active, must_reset_password, version FROM users WHERE email=$1`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	u, err := scanUser(m.DB.QueryRowContext(ctx, query, email).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return u, nil
}

// UserIssueStats is one member's all-time issue totals, for the Team page's
// per-member figures.
//
// This exists because those figures used to be counted in the browser out of
// every issue the app had loaded. Once the issue lists page their results, the
// browser no longer holds every issue and counting them there would silently
// under-report -- a member with 40 issues would show 3, and nothing would look
// broken. The count belongs next to the rows.
type UserIssueStats struct {
	UserID   int64 `json:"user_id"`
	Total    int   `json:"total"`
	Resolved int   `json:"resolved"`
	Minutes  int   `json:"minutes"`
}

// GetIssueStats returns all-time per-member totals, one row per user who has
// logged something. Members with no issues are absent rather than zero-filled,
// so the caller can tell "nobody" from "nobody has logged anything yet" and
// decide its own fallback.
func (m UserModel) GetIssueStats() ([]*UserIssueStats, error) {
	query := `
		SELECT logged_by,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE status = 'Ok') AS resolved,
		       COALESCE(SUM(time_minutes), 0) AS minutes
		FROM issues
		GROUP BY logged_by
		ORDER BY logged_by`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := []*UserIssueStats{}
	for rows.Next() {
		var s UserIssueStats
		if err := rows.Scan(&s.UserID, &s.Total, &s.Resolved, &s.Minutes); err != nil {
			return nil, err
		}
		stats = append(stats, &s)
	}
	return stats, rows.Err()
}

// GetAll returns up to limit users, plus the total number of accounts.
//
// Ordered oldest-first so the list is stable between requests: with only
// created_at as a tiebreaker two accounts created in the same transaction could
// swap places, and "load more" would then show one of them twice.
func (m UserModel) GetAll(limit int) ([]*User, int, error) {
	ctx, cancel := listContext()
	defer cancel()

	total, err := CountMatching(ctx, m.DB, `FROM users`, nil)
	if err != nil {
		return nil, 0, err
	}

	query := `SELECT id, created_at, name, email, role, avatar_idx, avatar_data, language, active,
		must_reset_password, version
		FROM users ORDER BY created_at ASC, id ASC
		LIMIT $1`
	rows, err := m.DB.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := []*User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.CreatedAt, &u.Name, &u.Email,
			&u.Role, &u.AvatarIdx, &u.AvatarData, &u.Language, &u.Active,
			&u.MustResetPassword, &u.Version); err != nil {
			return nil, 0, err
		}
		users = append(users, &u)
	}
	return users, total, rows.Err()
}

func (m UserModel) Update(user *User) error {
	query := `UPDATE users SET name=$1, email=$2, password_hash=$3, role=$4,
		avatar_idx=$5, avatar_data=$6, language=$7, active=$8, version=version+1
		WHERE id=$9 AND version=$10 RETURNING version`
	args := []any{user.Name, user.Email, user.Password.hash, user.Role,
		user.AvatarIdx, user.AvatarData, user.Language, user.Active, user.ID, user.Version}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&user.Version)
	if err != nil {
		switch {
		case err.Error() == `pq: duplicate key value violates unique constraint "users_email_key"`:
			return ErrDuplicateEmail
		case errors.Is(err, sql.ErrNoRows):
			return ErrEditConflict
		default:
			return err
		}
	}
	return nil
}

// scanUser reads a users row in the column order every query here uses. Every
// SELECT and RETURNING on this table goes through it, so adding a column is a
// one-line change rather than a hunt for a Scan that no longer lines up.
func scanUser(scan func(...any) error) (*User, error) {
	var u User
	err := scan(&u.ID, &u.CreatedAt, &u.Name, &u.Email, &u.Password.hash,
		&u.Role, &u.AvatarIdx, &u.AvatarData, &u.Language, &u.Active,
		&u.MustResetPassword, &u.Version)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (m UserModel) GetForToken(tokenScope, tokenPlaintext string) (*User, error) {
	tokenHash := sha256.Sum256([]byte(tokenPlaintext))
	// must_reset_password=false is not redundant with revoking tokens on reset.
	// A token issued in the instant before the reset commits would otherwise
	// stay usable, which is the one case the reset exists to close.
	query := `SELECT users.id, users.created_at, users.name, users.email, users.password_hash,
		users.role, users.avatar_idx, users.avatar_data, users.language, users.active,
		users.must_reset_password, users.version
		FROM users INNER JOIN tokens ON users.id = tokens.user_id
		WHERE tokens.hash=$1 AND tokens.scope=$2 AND tokens.expiry>$3
			AND users.active=true AND users.must_reset_password=false`
	args := []any{tokenHash[:], tokenScope, time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	u, err := scanUser(m.DB.QueryRowContext(ctx, query, args...).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return u, nil
}

func (m UserModel) Count() (int64, error) {
	var count int64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

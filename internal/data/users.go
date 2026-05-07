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
	Version    int       `json:"-"`
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
		avatar_idx, avatar_data, language, active, version FROM users WHERE id=$1`
	var u User
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&u.ID, &u.CreatedAt, &u.Name, &u.Email, &u.Password.hash,
		&u.Role, &u.AvatarIdx, &u.AvatarData, &u.Language, &u.Active, &u.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (m UserModel) GetByEmail(email string) (*User, error) {
	query := `SELECT id, created_at, name, email, password_hash, role,
		avatar_idx, avatar_data, language, active, version FROM users WHERE email=$1`
	var u User
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, query, email).Scan(
		&u.ID, &u.CreatedAt, &u.Name, &u.Email, &u.Password.hash,
		&u.Role, &u.AvatarIdx, &u.AvatarData, &u.Language, &u.Active, &u.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (m UserModel) GetAll() ([]*User, error) {
	query := `SELECT id, created_at, name, email, role, avatar_idx, avatar_data, language, active, version
		FROM users ORDER BY created_at ASC`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.CreatedAt, &u.Name, &u.Email,
			&u.Role, &u.AvatarIdx, &u.AvatarData, &u.Language, &u.Active, &u.Version); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
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

func (m UserModel) GetForToken(tokenScope, tokenPlaintext string) (*User, error) {
	tokenHash := sha256.Sum256([]byte(tokenPlaintext))
	query := `SELECT users.id, users.created_at, users.name, users.email, users.password_hash,
		users.role, users.avatar_idx, users.avatar_data, users.language, users.active, users.version
		FROM users INNER JOIN tokens ON users.id = tokens.user_id
		WHERE tokens.hash=$1 AND tokens.scope=$2 AND tokens.expiry>$3 AND users.active=true`
	args := []any{tokenHash[:], tokenScope, time.Now()}
	var u User
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, query, args...).Scan(
		&u.ID, &u.CreatedAt, &u.Name, &u.Email, &u.Password.hash,
		&u.Role, &u.AvatarIdx, &u.AvatarData, &u.Language, &u.Active, &u.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (m UserModel) Count() (int64, error) {
	var count int64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

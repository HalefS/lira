package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"time"
)

const ScopeAuthentication = "authentication"

type Token struct {
	Plaintext string    `json:"token"`
	Hash      []byte    `json:"-"`
	UserID    int64     `json:"-"`
	Expiry    time.Time `json:"expiry"`
	Scope     string    `json:"-"`
}

// hashToken is the one place a token string becomes the bytes stored in the
// database. Both issuing a token and revoking one go through it, so a revoke
// cannot quietly hash differently from the issue it is trying to undo.
func hashToken(plaintext string) []byte {
	hash := sha256.Sum256([]byte(plaintext))
	return hash[:]
}

func generateToken(userID int64, ttl time.Duration, scope string) (*Token, error) {
	token := &Token{
		UserID: userID,
		Expiry: time.Now().Add(ttl),
		Scope:  scope,
	}

	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, err
	}

	token.Plaintext = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes)
	token.Hash = hashToken(token.Plaintext)

	return token, nil
}

type TokenModel struct {
	DB *sql.DB
}

func (m TokenModel) New(userID int64, ttl time.Duration, scope string) (*Token, error) {
	token, err := generateToken(userID, ttl, scope)
	if err != nil {
		return nil, err
	}
	return token, m.Insert(token)
}

func (m TokenModel) Insert(token *Token) error {
	query := `
		INSERT INTO tokens (hash, user_id, expiry, scope)
		VALUES ($1, $2, $3, $4)`

	args := []any{token.Hash, token.UserID, token.Expiry, token.Scope}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, args...)
	return err
}

func (m TokenModel) DeleteAllForUser(scope string, userID int64) error {
	query := `DELETE FROM tokens WHERE scope = $1 AND user_id = $2`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, scope, userID)
	return err
}

// DeleteByPlaintext revokes exactly the token that was presented, rather than
// every token the user holds.
//
// That distinction is the point. This app signs in one device at a time, so
// "delete all for this user" would be equivalent today -- but it is the wrong
// shape for a sign-out: revoking what was actually used is what a sign-out means,
// and it stays correct if concurrent sessions are ever allowed.
//
// Matching on scope as well as hash means a reset code or any other future token
// kind can never be destroyed by an authentication sign-out.
func (m TokenModel) DeleteByPlaintext(scope, plaintext string) error {
	if plaintext == "" {
		return nil
	}

	query := `DELETE FROM tokens WHERE scope = $1 AND hash = $2`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, scope, hashToken(plaintext))
	return err
}

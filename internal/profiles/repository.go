// Package profiles owns editable member information and privacy preferences.
package profiles

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("profile not found")

type Profile struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	FirstName        string `json:"firstName"`
	LastName         string `json:"lastName"`
	EmailVerified    bool   `json:"emailVerified"`
	HasPassword      bool   `json:"hasPassword"`
	DirectoryVisible bool   `json:"directoryVisible"`
}
type Update struct {
	FirstName        *string `json:"firstName"`
	LastName         *string `json:"lastName"`
	DirectoryVisible *bool   `json:"directoryVisible"`
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = "id,email,first_name,last_name,email_verified_at IS NOT NULL,password_hash IS NOT NULL,directory_visible"

func scan(row *sql.Row) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.Email, &p.FirstName, &p.LastName, &p.EmailVerified, &p.HasPassword, &p.DirectoryVisible)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	return p, err
}
func (r *Repository) Get(ctx context.Context, actor string) (Profile, error) {
	return scan(r.db.QueryRowContext(ctx, "SELECT "+columns+" FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL", actor))
}

// Atomic partial updates preserve concurrent edits to fields omitted by this request.
func (r *Repository) Update(ctx context.Context, actor string, in Update) (Profile, error) {
	return scan(r.db.QueryRowContext(ctx, `UPDATE accounts SET first_name=COALESCE($2,first_name),last_name=COALESCE($3,last_name),directory_visible=COALESCE($4,directory_visible) WHERE id=$1 AND email_verified_at IS NOT NULL RETURNING `+columns, actor, in.FirstName, in.LastName, in.DirectoryVisible))
}

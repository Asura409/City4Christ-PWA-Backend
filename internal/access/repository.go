package access

import (
	"context"
	"database/sql"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Assignments uses one PostgreSQL statement so account verification and all role
// rows come from the same snapshot. It never reads another account on behalf of
// an actor: the HTTP/service caller supplies only its authenticated account ID.
func (r *Repository) Assignments(ctx context.Context, accountID string) ([]Assignment, error) {
	if !validID(accountID) {
		return nil, ErrForbidden
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.role,r.scope FROM accounts a LEFT JOIN account_roles r ON r.account_id=a.id WHERE a.id=$1 AND a.email_verified_at IS NOT NULL ORDER BY r.role,r.scope`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assignments := []Assignment{}
	found := false
	for rows.Next() {
		found = true
		var role, scope sql.NullString
		if err = rows.Scan(&role, &scope); err != nil {
			return nil, err
		}
		if role.Valid && scope.Valid {
			assignments = append(assignments, Assignment{Role: role.String, Scope: scope.String})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrForbidden
	}
	return assignments, nil
}

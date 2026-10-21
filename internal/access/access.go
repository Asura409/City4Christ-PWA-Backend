// Package access owns role delegation and scoped authorization.
package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrForbidden = errors.New("permission denied")
	ErrInvalid   = errors.New("invalid role assignment")
)

type Change struct {
	Role   string `json:"role"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}
type Assignment struct {
	Role  string `json:"role"`
	Scope string `json:"scope"`
}
type Service struct{ db *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{db: db} }
func validID(id string) bool {
	v, err := uuid.Parse(id)
	return err == nil && v.String() == id && v != uuid.Nil
}

// CheckTx locks the role and verified account until the caller's transaction ends.
// Permission names are server-defined. Unknown names and mismatched scopes deny.
func CheckTx(ctx context.Context, tx *sql.Tx, actor, permission, scope string) error {
	if !validID(actor) {
		return ErrForbidden
	}
	roles := rolesForPermission(permission, scope)
	if len(roles) == 0 {
		return ErrForbidden
	}
	var role string
	err := tx.QueryRowContext(ctx, `SELECT r.role FROM account_roles r JOIN accounts a ON a.id=r.account_id WHERE r.account_id=$1 AND r.scope=$2 AND r.role=ANY($3::text[]) AND a.email_verified_at IS NOT NULL ORDER BY r.role LIMIT 1 FOR SHARE OF r,a`, actor, scope, roles).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	return err
}
func (s *Service) Check(ctx context.Context, actor, permission, scope string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return CheckTx(ctx, tx, actor, permission, scope)
}
func (s *Service) Grant(ctx context.Context, actor, target string, in Change) (bool, error) {
	return s.change(ctx, actor, target, in, true, nil)
}
func (s *Service) Revoke(ctx context.Context, actor, target string, in Change) (bool, error) {
	return s.change(ctx, actor, target, in, false, nil)
}

// GrantWithGuard checks session freshness inside the same transaction as the
// role policy, assignment and audit. Internal operator callers use Grant.
func (s *Service) GrantWithGuard(ctx context.Context, actor, target string, in Change, guard func(context.Context, *sql.Tx) error) (bool, error) {
	return s.change(ctx, actor, target, in, true, guard)
}
func (s *Service) RevokeWithGuard(ctx context.Context, actor, target string, in Change, guard func(context.Context, *sql.Tx) error) (bool, error) {
	return s.change(ctx, actor, target, in, false, guard)
}
func (s *Service) change(ctx context.Context, actor, target string, in Change, grant bool, guard func(context.Context, *sql.Tx) error) (bool, error) {
	if actor == target {
		return false, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "roles.manage", ""); err != nil {
		return false, err
	}
	if guard != nil {
		if err = guard(ctx, tx); err != nil {
			return false, err
		}
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !validID(target) || !utf8.ValidString(in.Reason) || strings.ContainsRune(in.Reason, 0) || utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 500 {
		return false, ErrInvalid
	}
	if !((in.Role == "content_editor" && in.Scope == "") || (in.Role == "ministry_leader" && validID(in.Scope))) {
		return false, ErrInvalid
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_scopes WHERE id=$1)`, in.Scope).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, ErrInvalid
	}
	// Serialize role mutations without taking an exclusive account lock.
	// Shared account locks remain compatible with authorization and other admins.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('access-target/' || $1, 0))`, target); err != nil {
		return false, err
	}
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1 FOR SHARE`, target).Scan(&verified); errors.Is(err, sql.ErrNoRows) {
		return false, ErrInvalid
	} else if err != nil {
		return false, err
	}
	if grant && !verified {
		return false, ErrInvalid
	}
	var result sql.Result
	if grant {
		result, err = tx.ExecContext(ctx, `INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, target, in.Role, in.Scope)
	} else {
		result, err = tx.ExecContext(ctx, `DELETE FROM account_roles WHERE account_id=$1 AND role=$2 AND scope=$3`, target, in.Role, in.Scope)
	}
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		action := "revoked"
		if grant {
			action = "granted"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO access_audit_events(actor_id,target_id,role,scope,action,reason) VALUES($1,$2,$3,$4,$5,$6)`, actor, target, in.Role, in.Scope, action, in.Reason); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}
func (s *Service) List(ctx context.Context, actor, target string) ([]Assignment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "roles.manage", ""); err != nil {
		return nil, err
	}
	if !validID(target) {
		return nil, ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT role,scope FROM account_roles WHERE account_id=$1 ORDER BY role,scope`, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Assignment{}
	for rows.Next() {
		var a Assignment
		if err = rows.Scan(&a.Role, &a.Scope); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrLastAdministrator = errors.New("last active super administrator")
var ErrBootstrapConflict = errors.New("church administrator already bootstrapped")

func lockRoleChanges(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('church/access-role-mutations',0))`)
	return err
}
func validReason(reason string) bool {
	return utf8.ValidString(reason) && !strings.ContainsRune(reason, 0) && utf8.RuneCountInString(reason) >= 1 && utf8.RuneCountInString(reason) <= 500
}
func protectedChurchRole(role string) bool {
	switch role {
	case "super_administrator", "assistant_pastor", "staff", "finance_officer":
		return true
	}
	return false
}
func lockChurchTarget(ctx context.Context, tx *sql.Tx, target string, requireMFA bool) error {
	var verified bool
	err := tx.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1 FOR SHARE`, target).Scan(&verified)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if requireMFA {
		var enabled bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mfa_factors WHERE account_id=$1 AND confirmed_at IS NOT NULL)`, target).Scan(&enabled); err != nil {
			return err
		}
		if !verified || !enabled {
			return ErrInvalid
		}
	}
	return nil
}

// BootstrapChurchAdmin is an explicit operator-only one-time installation action.
// It is never called automatically and creates no initial account or MFA factor.
func (s *Service) BootstrapChurchAdmin(ctx context.Context, target, reason string) error {
	reason = strings.TrimSpace(reason)
	if !validID(target) || !validReason(reason) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockRoleChanges(ctx, tx); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM church_role_bootstrap) OR EXISTS(SELECT 1 FROM account_roles WHERE role='super_administrator')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrBootstrapConflict
	}
	if err = lockChurchTarget(ctx, tx, target, true); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_roles(account_id,role,scope) VALUES($1,'super_administrator','')`, target); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO church_role_bootstrap(id,account_id,reason) VALUES(true,$1,$2)`, target, reason); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) ChurchGrant(ctx context.Context, actor, target string, in Change, guard func(context.Context, *sql.Tx) error) (bool, error) {
	return s.churchChange(ctx, actor, target, in, true, guard)
}
func (s *Service) ChurchRevoke(ctx context.Context, actor, target string, in Change, guard func(context.Context, *sql.Tx) error) (bool, error) {
	return s.churchChange(ctx, actor, target, in, false, guard)
}
func (s *Service) churchChange(ctx context.Context, actor, target string, in Change, grant bool, guard func(context.Context, *sql.Tx) error) (bool, error) {
	if guard == nil {
		return false, ErrForbidden
	}
	if actor == target && (grant || in.Role != "super_administrator") {
		return false, ErrForbidden
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !validID(target) || !validReason(in.Reason) {
		return false, ErrInvalid
	}
	permission := "roles.operational.manage"
	if protectedChurchRole(in.Role) {
		if in.Scope != "" {
			return false, ErrInvalid
		}
		permission = "roles.privileged.manage"
	} else if in.Role != "ministry_leader" || !validID(in.Scope) {
		return false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = lockRoleChanges(ctx, tx); err != nil {
		return false, err
	}
	if err = CheckTx(ctx, tx, actor, permission, ""); err != nil {
		return false, err
	}
	if err = lockChurchTarget(ctx, tx, target, grant && protectedChurchRole(in.Role)); err != nil {
		return false, err
	}
	if grant && !protectedChurchRole(in.Role) {
		var verified bool
		if err = tx.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1`, target).Scan(&verified); err != nil {
			return false, err
		}
		if !verified {
			return false, ErrInvalid
		}
	}
	var scopeExists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_scopes WHERE id=$1)`, in.Scope).Scan(&scopeExists); err != nil {
		return false, err
	}
	if !scopeExists {
		return false, ErrInvalid
	}
	if !grant && in.Role == "super_administrator" {
		// Freeze other administrators' MFA enrollment while deciding whether this
		// resignation would remove the final effective administrator.
		rows, lockErr := tx.QueryContext(ctx, `SELECT a.id FROM accounts a JOIN account_roles r ON r.account_id=a.id WHERE r.role='super_administrator' AND r.scope='' ORDER BY a.id FOR SHARE OF a`)
		if lockErr != nil {
			return false, lockErr
		}
		for rows.Next() {
			var id string
			if lockErr = rows.Scan(&id); lockErr != nil {
				rows.Close()
				return false, lockErr
			}
		}
		lockErr = rows.Err()
		rows.Close()
		if lockErr != nil {
			return false, lockErr
		}
		var active, count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM account_roles r JOIN accounts a ON a.id=r.account_id JOIN mfa_factors m ON m.account_id=a.id AND m.confirmed_at IS NOT NULL WHERE r.role='super_administrator' AND r.scope='' AND a.email_verified_at IS NOT NULL`).Scan(&count); err != nil {
			return false, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM account_roles r JOIN accounts a ON a.id=r.account_id JOIN mfa_factors m ON m.account_id=a.id AND m.confirmed_at IS NOT NULL WHERE r.role='super_administrator' AND r.scope='' AND r.account_id=$1 AND a.email_verified_at IS NOT NULL`, target).Scan(&active); err != nil {
			return false, err
		}
		if active > 0 && count <= 1 {
			return false, ErrLastAdministrator
		}
	}
	// This is deliberately after all potentially blocking locks and validations.
	if err = guard(ctx, tx); err != nil {
		return false, err
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

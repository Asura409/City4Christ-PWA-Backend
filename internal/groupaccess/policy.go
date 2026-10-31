// Package groupaccess centralizes group audience and exact-scope management checks.
package groupaccess

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

var ErrDenied = errors.New("group content access denied")

// CheckTx permits management without inventing group membership. Leadership
// eligibility and active group/account status are checked even for global staff.
func CheckTx(ctx context.Context, tx *sql.Tx, actor, groupID, permission string) error {
	if groupID == "" {
		return access.CheckTx(ctx, tx, actor, permission, "")
	}
	var eligible bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups g JOIN accounts a ON a.id=$2 WHERE g.id=$1 AND g.archived_at IS NULL AND a.email_verified_at IS NOT NULL AND (g.audience='church' OR EXISTS(SELECT 1 FROM account_roles r WHERE r.account_id=a.id AND ((r.role IN ('super_administrator','assistant_pastor') AND r.scope='') OR r.role='ministry_leader'))))`, groupID, actor).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrDenied
	}
	if err = access.CheckTx(ctx, tx, actor, permission, ""); err == nil {
		return nil
	} else if !errors.Is(err, access.ErrForbidden) {
		return err
	}
	scoped := "ministry.members.manage"
	if permission == "groups.content.moderate" {
		scoped = permission
	}
	if err = access.CheckTx(ctx, tx, actor, scoped, groupID); errors.Is(err, access.ErrForbidden) {
		return ErrDenied
	}
	return err
}

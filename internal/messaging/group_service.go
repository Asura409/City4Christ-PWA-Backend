package messaging

import (
	"context"
	"database/sql"
	"errors"
)

func (r *Repository) groupChatAccess(ctx context.Context, tx *sql.Tx, groupID, actor string) (string, error) {
	var audience string
	err := tx.QueryRowContext(ctx, `SELECT audience FROM groups WHERE id=$1 AND archived_at IS NULL FOR SHARE`, groupID).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1),false)`, actor).Scan(&verified); err != nil {
		return "", err
	}
	if !verified {
		return "", ErrForbidden
	}
	if audience == "church" {
		var status string
		err = tx.QueryRowContext(ctx, `SELECT status FROM group_memberships WHERE group_id=$1 AND account_id=$2 FOR SHARE`, groupID, actor).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && status != "active") {
			return "", ErrNotFound
		}
		if err != nil {
			return "", err
		}
		return audience, nil
	}
	var eligible bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=$1 AND ((role IN ('super_administrator','assistant_pastor') AND scope='') OR role='ministry_leader'))`, actor).Scan(&eligible); err != nil {
		return "", err
	}
	if !eligible {
		return "", ErrNotFound
	}
	return audience, nil
}

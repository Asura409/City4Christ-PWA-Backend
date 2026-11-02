package groups

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
)

type RemovalInput struct {
	Reason string `json:"reason"`
}

// RemoveMember ends an active membership after verifying group-scoped authority.
// The reason and audit event are committed with the membership change.
func (s *Service) RemoveMember(ctx context.Context, actor, groupID, target, reason string) (bool, error) {
	reason = strings.TrimSpace(reason)
	if !validID(actor) || !validID(groupID) || !validID(target) || actor == target || !utf8.ValidString(reason) || len([]rune(reason)) < 1 || len([]rune(reason)) > 500 || strings.ContainsRune(reason, 0) {
		return false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var audience string
	err = tx.QueryRowContext(ctx, `SELECT audience FROM groups WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, groupID).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if audience == "leadership" {
		eligible, e := leadershipEligible(ctx, tx, actor)
		if e != nil {
			return false, e
		}
		if !eligible {
			return false, ErrNotFound
		}
	}
	err = access.CheckTx(ctx, tx, actor, "groups.members.manage", "")
	if err != nil {
		if !errors.Is(err, access.ErrForbidden) {
			return false, err
		}
		err = access.CheckTx(ctx, tx, actor, "ministry.members.manage", groupID)
		if errors.Is(err, access.ErrForbidden) {
			return false, ErrForbidden
		}
		if err != nil {
			return false, err
		}
	}
	var targetIsLeader bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=$1 AND role='ministry_leader' AND scope=$2)`, target, groupID).Scan(&targetIsLeader); err != nil {
		return false, err
	}
	if targetIsLeader {
		return false, ErrForbidden
	}
	var current string
	err = tx.QueryRowContext(ctx, `SELECT status FROM group_memberships WHERE group_id=$1 AND account_id=$2 FOR UPDATE`, groupID, target).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if current == "left" {
		return false, tx.Commit()
	}
	if current != "active" {
		return false, ErrInvalid
	}
	changedAt := s.now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE group_memberships SET status='left',decided_at=$3,decided_by=$4 WHERE group_id=$1 AND account_id=$2`, groupID, target, changedAt, actor); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO group_audit_events(group_id,actor_id,target_id,action,reason) VALUES($1,$2,$3,'removed',$4)`, groupID, actor, target, reason); err != nil {
		return false, err
	}
	var groupName string
	if err = tx.QueryRowContext(ctx, `SELECT name FROM groups WHERE id=$1`, groupID).Scan(&groupName); err != nil {
		return false, err
	}
	if _, err = notifications.NewRepository(s.db).CreateTx(ctx, tx, notifications.CreateInput{
		AccountID: target, Kind: "group.membership_removed", Title: "Your group membership ended",
		Body: "Your membership in " + groupName + " has ended.", ResourcePath: "/#/groups/" + groupID,
		DedupKey: "group-membership-removed:" + groupID + ":" + target + ":" + changedAt.Format(time.RFC3339Nano),
	}); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

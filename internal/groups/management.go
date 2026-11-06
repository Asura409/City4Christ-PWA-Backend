package groups

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

type UpdateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	JoinPolicy  string `json:"joinPolicy"`
	Reason      string `json:"reason"`
}

type OwnershipInput struct {
	AccountID string `json:"accountId"`
	Reason    string `json:"reason"`
}

type Analytics struct {
	ActiveMembers   int `json:"activeMembers"`
	PendingRequests int `json:"pendingRequests"`
	Posts           int `json:"posts"`
	Events          int `json:"events"`
	Messages        int `json:"messages"`
}

func checkGroupPermission(ctx context.Context, tx *sql.Tx, actor, groupID, permission string) error {
	var audience string
	if err := tx.QueryRowContext(ctx, `SELECT audience FROM groups WHERE id=$1 AND archived_at IS NULL`, groupID).Scan(&audience); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if audience == "leadership" {
		eligible, err := leadershipEligible(ctx, tx, actor)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrNotFound
		}
	}
	if err := access.CheckTx(ctx, tx, actor, permission, groupID); err == nil {
		return nil
	}
	if err := access.CheckTx(ctx, tx, actor, permission, ""); err != nil {
		if errors.Is(err, access.ErrForbidden) {
			return ErrForbidden
		}
		return err
	}
	return nil
}

func (s *Service) Update(ctx context.Context, actor, groupID string, in UpdateInput) (Group, error) {
	in.Name, in.Description, in.JoinPolicy, in.Reason = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.JoinPolicy), strings.TrimSpace(in.Reason)
	if !validID(actor) || !validID(groupID) || !utf8.ValidString(in.Name) || len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 120 || len(in.Description) > 2000 || !utf8.ValidString(in.Description) || (in.JoinPolicy != "open" && in.JoinPolicy != "approval" && in.JoinPolicy != "closed") || !utf8.ValidString(in.Reason) || len([]rune(in.Reason)) < 1 || len([]rune(in.Reason)) > 500 {
		return Group{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback()
	if err = checkGroupPermission(ctx, tx, actor, groupID, "groups.settings.manage"); err != nil {
		return Group{}, err
	}
	var g Group
	err = tx.QueryRowContext(ctx, `UPDATE groups SET name=$2,description=$3,join_policy=$4 WHERE id=$1 RETURNING id,name,description,audience,join_policy,created_at`, groupID, in.Name, in.Description, in.JoinPolicy).Scan(&g.ID, &g.Name, &g.Description, &g.Audience, &g.JoinPolicy, &g.CreatedAt)
	if err != nil {
		return Group{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO group_audit_events(group_id,actor_id,action,reason) VALUES($1,$2,'updated',$3)`, groupID, actor, in.Reason); err != nil {
		return Group{}, err
	}
	if err = tx.Commit(); err != nil {
		return Group{}, err
	}
	return g, nil
}

func (s *Service) TransferOwnership(ctx context.Context, actor, groupID string, in OwnershipInput) error {
	in.AccountID, in.Reason = strings.TrimSpace(in.AccountID), strings.TrimSpace(in.Reason)
	if !validID(actor) || !validID(groupID) || !validID(in.AccountID) || actor == in.AccountID || !utf8.ValidString(in.Reason) || len([]rune(in.Reason)) < 1 || len([]rune(in.Reason)) > 500 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkGroupPermission(ctx, tx, actor, groupID, "groups.ownership.transfer"); err != nil {
		return err
	}
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL AND deletion_requested_at IS NULL FROM accounts WHERE id=$1`, in.AccountID).Scan(&verified); errors.Is(err, sql.ErrNoRows) || !verified {
		return ErrInvalid
	} else if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE groups SET created_by=$2 WHERE id=$1`, groupID, in.AccountID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO group_audit_events(group_id,actor_id,target_id,action,reason) VALUES($1,$2,$3,'ownership_transferred',$4)`, groupID, actor, in.AccountID, in.Reason); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Analytics(ctx context.Context, actor, groupID string) (Analytics, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Analytics{}, err
	}
	defer tx.Rollback()
	if err = checkGroupPermission(ctx, tx, actor, groupID, "groups.analytics.read"); err != nil {
		return Analytics{}, err
	}
	var a Analytics
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status='active'), count(*) FILTER (WHERE status='pending') FROM group_memberships WHERE group_id=$1`, groupID).Scan(&a.ActiveMembers, &a.PendingRequests)
	if err != nil {
		return Analytics{}, err
	}
	// Posts/events/messages are intentionally scoped to this group and only expose counts.
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM church_posts WHERE group_id=$1 AND deleted_at IS NULL`, groupID).Scan(&a.Posts)
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM church_events WHERE group_id=$1`, groupID).Scan(&a.Events)
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM group_chat_messages WHERE group_id=$1 AND deleted_at IS NULL`, groupID).Scan(&a.Messages)
	if err = tx.Commit(); err != nil {
		return Analytics{}, err
	}
	return a, nil
}

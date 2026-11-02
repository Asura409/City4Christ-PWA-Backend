package access

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

type MemberAccessEvent struct {
	ID              string    `json:"id"`
	ActorID         string    `json:"actorId"`
	Action          string    `json:"action"`
	SensitiveFields []string  `json:"sensitiveFields"`
	Reason          string    `json:"reason,omitempty"`
	OccurredAt      time.Time `json:"occurredAt"`
}

type MemberAccessAuditPage struct {
	Data       []MemberAccessEvent `json:"data"`
	NextCursor string              `json:"nextCursor"`
}

// MemberAccessAudit returns an audit page for one verified active account.
// The cursor is the audit row's numeric ID, not an account credential.
func (s *Service) MemberAccessAudit(ctx context.Context, actor, memberID, cursor string, limit int) (MemberAccessAuditPage, error) {
	page := MemberAccessAuditPage{Data: []MemberAccessEvent{}}
	if !validID(memberID) {
		return page, ErrMemberNotFound
	}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	var before int64
	if cursor != "" {
		parsed, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil || parsed < 1 {
			return page, ErrInvalid
		}
		before = parsed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "members.records.audit.read", ""); err != nil {
		return page, err
	}
	var activeID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL FOR SHARE`, memberID).Scan(&activeID)
	if errors.Is(err, sql.ErrNoRows) {
		return page, ErrMemberNotFound
	}
	if err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,actor_id::text,action,sensitive_fields,COALESCE(reason,''),occurred_at FROM member_record_audit_events WHERE member_id=$1 AND action IN ('search_result','detail_view','name_update') AND ($2=0 OR id<$2) ORDER BY id DESC LIMIT $3`, memberID, before, limit+1)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var item MemberAccessEvent
		if err = rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.SensitiveFields, &item.Reason, &item.OccurredAt); err != nil {
			rows.Close()
			return page, err
		}
		page.Data = append(page.Data, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return page, err
	}
	if err = rows.Close(); err != nil {
		return page, err
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[len(page.Data)-1].ID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO member_record_audit_events(actor_id,member_id,action,sensitive_fields) VALUES($1,$2,'audit_view',ARRAY[]::text[])`, actor, memberID); err != nil {
		return MemberAccessAuditPage{}, err
	}
	if err = tx.Commit(); err != nil {
		return MemberAccessAuditPage{}, err
	}
	return page, nil
}

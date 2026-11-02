package visitors

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
)

func (s *Service) Assign(ctx context.Context, actor, visitorID string, in AssignmentInput) (Summary, error) {
	if !validID(visitorID) {
		return Summary{}, ErrNotFound
	}
	in.AccountID = strings.TrimSpace(in.AccountID)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version < 1 || !validText(in.Reason, 500) || len([]rune(in.Reason)) < 3 || (in.AccountID != "" && !validID(in.AccountID)) {
		return Summary{}, ErrInvalid
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, err
	}
	defer tx.Rollback()
	if err = permit(ctx, tx, actor); err != nil {
		return Summary{}, err
	}
	var v Summary
	var previous string
	var consent string
	err = tx.QueryRowContext(ctx, `SELECT id,status,version,created_at,expires_at,COALESCE(assigned_to::text,''),consent_version FROM visitor_intakes WHERE id=$1 AND sealed_payload IS NOT NULL AND expires_at>$2 FOR UPDATE`, visitorID, s.now().UTC()).Scan(&v.ID, &v.Status, &v.Version, &v.CreatedAt, &v.ExpiresAt, &previous, &consent)
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, err
	}
	if v.Version != in.Version {
		return Summary{}, ErrConflict
	}
	if consent != PolicyVersion || (v.Status != "new" && v.Status != "contacted") {
		return Summary{}, ErrInvalid
	}
	if in.AccountID != "" {
		var eligible string
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL FOR SHARE`, in.AccountID).Scan(&eligible)
		if errors.Is(err, sql.ErrNoRows) {
			return Summary{}, ErrInvalid
		}
		if err != nil {
			return Summary{}, err
		}
	}
	if previous == in.AccountID {
		v.AssignedTo = previous
		return v, tx.Commit()
	}
	v.AssignedTo = in.AccountID
	v.Version++
	if _, err = tx.ExecContext(ctx, `UPDATE visitor_intakes SET assigned_to=NULLIF($2,'')::uuid,version=version+1,updated_at=$3 WHERE id=$1`, visitorID, in.AccountID, s.now().UTC()); err != nil {
		return Summary{}, err
	}
	action := "assigned"
	if in.AccountID == "" {
		action = "unassigned"
	}
	if err = auditAssignment(ctx, tx, actor, v, action, previous, in.AccountID, in.Reason, s.now()); err != nil {
		return Summary{}, err
	}
	if in.AccountID != "" {
		if _, err = notifications.NewRepository(nil).CreateTx(ctx, tx, notifications.CreateInput{
			AccountID: in.AccountID, Kind: "visitor.assigned", Title: "Visitor follow-up assigned",
			Body: "A consented visitor follow-up task was assigned to you.", ResourcePath: "/#/visitors",
			DedupKey: "visitor-assignment:" + visitorID + ":" + strconv.Itoa(v.Version),
		}); err != nil {
			return Summary{}, err
		}
	}
	return v, tx.Commit()
}

func auditAssignment(ctx context.Context, tx *sql.Tx, actor string, v Summary, action, previous, current, reason string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO visitor_audit_events(visitor_id,actor_id,action,previous_status,status,version,occurred_at,previous_assignee_id,assigned_to,reason) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10)`, v.ID, actor, action, v.Status, v.Status, v.Version, now.UTC(), previous, current, reason)
	return err
}

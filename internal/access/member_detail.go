package access

import (
	"context"
	"database/sql"
	"errors"
)

// MemberDetail returns only the basic account identity approved for staff
// review. Email access is recorded in member_record_audit_events.
func (s *Service) MemberDetail(ctx context.Context, actor, memberID string) (MemberRecord, error) {
	var member MemberRecord
	if !validID(memberID) {
		return member, ErrMemberNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return member, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "members.records.read", ""); err != nil {
		return member, err
	}
	err = tx.QueryRowContext(ctx, `SELECT a.id,a.first_name,a.last_name,a.email,a.created_at,a.profile_version FROM accounts a WHERE a.id=$1 AND a.email_verified_at IS NOT NULL AND a.deletion_requested_at IS NULL FOR SHARE OF a`, memberID).
		Scan(&member.ID, &member.FirstName, &member.LastName, &member.Email, &member.JoinedAt, &member.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberRecord{}, ErrMemberNotFound
	}
	if err != nil {
		return MemberRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO member_record_audit_events(actor_id,member_id,action,sensitive_fields)VALUES($1,$2,'detail_view',ARRAY['email']::text[])`, actor, memberID); err != nil {
		return MemberRecord{}, err
	}
	if err = tx.Commit(); err != nil {
		return MemberRecord{}, err
	}
	return member, nil
}

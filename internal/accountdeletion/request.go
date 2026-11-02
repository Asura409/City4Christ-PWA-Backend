package accountdeletion

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

func (s *Service) Status(ctx context.Context, actor string) (Status, error) {
	out := Status{PolicyVersion: PolicyVersion, Notice: Notice}
	if !validID(actor) {
		return out, access.ErrForbidden
	}
	var verified bool
	if e := s.db.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1 AND deletion_requested_at IS NULL`, actor).Scan(&verified); errors.Is(e, sql.ErrNoRows) {
		return out, access.ErrForbidden
	} else if e != nil {
		return out, e
	}
	if !verified {
		return out, access.ErrForbidden
	}
	r, e := scanRequest(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM account_deletion_requests d WHERE account_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, actor))
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	out.Requested = r.Status == "requested" || r.Status == "reviewing"
	out.Request = &r
	return out, nil
}

func (s *Service) Request(ctx context.Context, actor string, confirm bool, policyVersion string, guard Guard) (Request, error) {
	if !validID(actor) || !confirm || policyVersion != PolicyVersion {
		return Request{}, ErrInvalid
	}
	if guard == nil {
		return Request{}, access.ErrForbidden
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Request{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('church/access-role-mutations',0))`); e != nil {
		return Request{}, e
	}
	var verified bool
	if e = tx.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1 FOR UPDATE`, actor).Scan(&verified); errors.Is(e, sql.ErrNoRows) {
		return Request{}, access.ErrForbidden
	} else if e != nil {
		return Request{}, e
	}
	if !verified {
		return Request{}, access.ErrForbidden
	}
	if e = guard(ctx, tx); e != nil {
		return Request{}, e
	}
	existing, e := scanRequest(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM account_deletion_requests d WHERE account_id=$1 AND status IN('requested','reviewing') ORDER BY created_at DESC,id DESC LIMIT 1`, actor))
	if e == nil {
		return existing, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Request{}, e
	}
	var active, count int
	e = tx.QueryRowContext(ctx, `SELECT count(*),count(*)FILTER(WHERE a.id=$1) FROM accounts a JOIN account_roles r ON r.account_id=a.id AND r.role='super_administrator' AND r.scope='' JOIN mfa_factors m ON m.account_id=a.id AND m.confirmed_at IS NOT NULL WHERE a.email_verified_at IS NOT NULL AND a.deletion_requested_at IS NULL`, actor).Scan(&count, &active)
	if e != nil {
		return Request{}, e
	}
	if active > 0 && count <= 1 {
		return Request{}, access.ErrLastAdministrator
	}
	now := s.now().UTC()
	var commitments bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pastor_bookings WHERE(member_id=$1 OR pastor_id=$1)AND status IN('pending','approved')AND ends_at>$2)`, actor, now).Scan(&commitments); e != nil {
		return Request{}, e
	}
	if commitments {
		return Request{}, ErrActiveCommitments
	}
	var requestAt time.Time
	if e = tx.QueryRowContext(ctx, `SELECT GREATEST($2::timestamptz,COALESCE((SELECT max(created_at)+interval '1 microsecond' FROM account_deletion_requests WHERE account_id=$1),$2::timestamptz))`, actor, now).Scan(&requestAt); e != nil {
		return Request{}, e
	}
	r, e := scanRequest(tx.QueryRowContext(ctx, `INSERT INTO account_deletion_requests AS d(account_id,policy_version,consent,created_at,updated_at)VALUES($1,$2,true,$3,$3)RETURNING `+columns, actor, PolicyVersion, requestAt))
	if e != nil {
		return Request{}, e
	}
	for _, query := range []string{`UPDATE accounts SET deletion_requested_at=$2,directory_visible=false WHERE id=$1`, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,$2)WHERE account_id=$1`, `UPDATE password_recoveries SET consumed_at=$2,token_digest=NULL WHERE account_id=$1`, `UPDATE email_verifications SET consumed_at=$2,code_digest=NULL WHERE account_id=$1`} {
		if _, e = tx.ExecContext(ctx, query, actor, requestAt); e != nil {
			return Request{}, e
		}
	}
	for _, table := range []string{"verification_email_outbox", "password_recovery_outbox", "notification_push_subscriptions"} {
		if _, e = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE account_id=$1`, actor); e != nil {
			return Request{}, e
		}
	}
	if e = s.audit(ctx, tx, actor, r, "requested"); e != nil {
		return Request{}, e
	}
	return r, tx.Commit()
}

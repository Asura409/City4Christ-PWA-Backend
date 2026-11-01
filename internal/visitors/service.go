package visitors

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"github.com/google/uuid"
	"time"
)

type Service struct {
	repo       *Repository
	protection *Protection
	now        func() time.Time
}

func NewService(repo *Repository, p *Protection, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo, p, now}
}
func (s *Service) Submit(ctx context.Context, in Submission) error {
	var e error
	in, e = normalize(in)
	if e != nil {
		return e
	}
	if s.protection == nil {
		return ErrUnavailable
	}
	tx, e := s.repo.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('visitor-submission:'||$1,0))`, in.SubmissionID); e != nil {
		return e
	}
	var key sql.NullString
	var digest []byte
	var expires time.Time
	e = tx.QueryRowContext(ctx, `SELECT key_id,request_digest,expires_at FROM visitor_intakes WHERE submission_id=$1`, in.SubmissionID).Scan(&key, &digest, &expires)
	if e == nil {
		if !key.Valid || !expires.After(s.now()) {
			return tx.Commit()
		}
		expected, e := s.protection.requestDigest(key.String, in)
		if e != nil {
			return e
		}
		if !hmac.Equal(expected, digest) {
			return ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	id := uuid.NewString()
	sealed, e := s.protection.seal(id, in)
	if e != nil {
		return e
	}
	digest, e = s.protection.requestDigest(s.protection.current, in)
	if e != nil {
		return e
	}
	now := s.now().UTC()
	v := Summary{ID: id, Status: "new", Version: 1, CreatedAt: now, ExpiresAt: now.Add(RetentionDays * 24 * time.Hour)}
	if _, e = tx.ExecContext(ctx, `INSERT INTO visitor_intakes(id,submission_id,sealed_payload,key_id,request_digest,consent_version,consent_text,created_at,updated_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9)`, id, in.SubmissionID, sealed, s.protection.current, digest, PolicyVersion, ConsentText, now, v.ExpiresAt); e != nil {
		return e
	}
	if e = audit(ctx, tx, "", v, "submitted", "", now); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT a.id::text FROM accounts a WHERE a.email_verified_at IS NOT NULL AND EXISTS(SELECT 1 FROM account_roles r WHERE r.account_id=a.id AND r.scope='' AND r.role IN ('super_administrator','assistant_pastor','staff'))`)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var actor string
		if e = rows.Scan(&actor); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, actor)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	for _, actor := range ids {
		if _, e = notifications.NewRepository(nil).CreateTx(ctx, tx, notifications.CreateInput{AccountID: actor, Kind: "visitor.received", Title: "Visitor follow-up", Body: "A visitor welcome form is ready for authorized review.", ResourcePath: "/#/visitors", DedupKey: "visitor:" + id}); e != nil {
			return e
		}
	}
	return tx.Commit()
}

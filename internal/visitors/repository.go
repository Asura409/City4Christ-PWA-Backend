package visitors

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db} }
func permit(ctx context.Context, tx *sql.Tx, actor string) error {
	if !validID(actor) {
		return ErrForbidden
	}
	err := access.CheckTx(ctx, tx, actor, "visitors.follow_up", "")
	if errors.Is(err, access.ErrForbidden) {
		return ErrForbidden
	}
	return err
}
func audit(ctx context.Context, tx *sql.Tx, actor string, v Summary, action, old string, now time.Time) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO visitor_audit_events(visitor_id,actor_id,action,previous_status,status,version,occurred_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7)`, v.ID, actor, action, old, v.Status, v.Version, now.UTC())
	return e
}
func PurgeExpired(ctx context.Context, db *sql.DB, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT id,status,version FROM visitor_intakes WHERE expires_at<=$1 AND sealed_payload IS NOT NULL ORDER BY expires_at,id LIMIT $2 FOR UPDATE SKIP LOCKED`, now.UTC(), limit)
	if e != nil {
		return 0, e
	}
	items := []Summary{}
	for rows.Next() {
		var v Summary
		if e = rows.Scan(&v.ID, &v.Status, &v.Version); e != nil {
			rows.Close()
			return 0, e
		}
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return 0, e
	}
	rows.Close()
	for _, v := range items {
		old := v.Status
		v.Status = "expired"
		v.Version++
		if _, e = tx.ExecContext(ctx, `UPDATE visitor_intakes SET sealed_payload=NULL,key_id=NULL,request_digest=NULL,status='expired',version=version+1,updated_at=$2 WHERE id=$1`, v.ID, now.UTC()); e != nil {
			return 0, e
		}
		if e = audit(ctx, tx, "", v, "expired", old, now); e != nil {
			return 0, e
		}
	}
	return len(items), tx.Commit()
}

package accountdeletion

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

func (s *Service) Restore(ctx context.Context, actor, id string, version int, reason string, guard Guard) (Request, error) {
	if !validID(actor) || !validID(id) || version < 1 {
		return Request{}, ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if !utf8.ValidString(reason) || utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 500 {
		return Request{}, ErrInvalid
	}
	for _, r := range reason {
		if unicode.IsControl(r) {
			return Request{}, ErrInvalid
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Request{}, e
	}
	defer tx.Rollback()
	if guard == nil {
		return Request{}, access.ErrForbidden
	}
	if e = access.CheckTx(ctx, tx, actor, "accounts.deletion.review", ""); e != nil {
		return Request{}, e
	}
	r, e := scanRequest(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM account_deletion_requests d WHERE d.id=$1 FOR UPDATE`, id))
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if e = guard(ctx, tx); e != nil {
		return Request{}, e
	}
	if r.Version != version || (r.Status != "requested" && r.Status != "reviewing") {
		return Request{}, ErrConflict
	}
	now := s.now().UTC()
	result, e := tx.ExecContext(ctx, `UPDATE accounts SET deletion_requested_at=NULL WHERE id=$1 AND deletion_requested_at IS NOT NULL`, r.AccountID)
	if e != nil {
		return Request{}, e
	}
	if count, e := result.RowsAffected(); e != nil {
		return Request{}, e
	} else if count != 1 {
		return Request{}, ErrConflict
	}
	r.Status = "restored"
	r.Version++
	r.UpdatedAt = now
	if _, e = tx.ExecContext(ctx, `UPDATE account_deletion_requests SET status=$2,version=$3,updated_at=$4 WHERE id=$1`, id, r.Status, r.Version, r.UpdatedAt); e != nil {
		return Request{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO account_deletion_audit_events(request_id,actor_id,action,status,version,occurred_at,reason)VALUES($1,$2,'restored',$3,$4,$5,$6)`, r.ID, actor, r.Status, r.Version, now, reason); e != nil {
		return Request{}, e
	}
	return r, tx.Commit()
}

func permit(ctx context.Context, tx *sql.Tx, actor string, guard Guard) error {
	if guard == nil {
		return access.ErrForbidden
	}
	if e := access.CheckTx(ctx, tx, actor, "accounts.deletion.review", ""); e != nil {
		return e
	}
	return guard(ctx, tx)
}
func (s *Service) List(ctx context.Context, actor, status, cursor string, limit int, guard Guard) (Page, error) {
	out := Page{Items: []Summary{}}
	if status == "" {
		status = "requested"
	}
	if limit == 0 {
		limit = 20
	}
	if (status != "requested" && status != "reviewing") || limit < 1 || limit > 100 || (cursor != "" && !validID(cursor)) {
		return out, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = permit(ctx, tx, actor, guard); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+columns+`,a.first_name,a.last_name FROM account_deletion_requests d JOIN accounts a ON a.id=d.account_id WHERE d.status=$1 AND($2=''OR d.id>NULLIF($2,'')::uuid)ORDER BY d.id LIMIT $3`, status, cursor, limit+1)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v Summary
		if e = rows.Scan(&v.ID, &v.AccountID, &v.Status, &v.Version, &v.PolicyVersion, &v.Consent, &v.CreatedAt, &v.UpdatedAt, &v.FirstName, &v.LastName); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.NextCursor = out.Items[limit-1].ID
	}
	for _, v := range out.Items {
		if e = s.audit(ctx, tx, actor, v.Request, "listed"); e != nil {
			return Page{}, e
		}
	}
	return out, tx.Commit()
}
func (s *Service) Detail(ctx context.Context, actor, id string, guard Guard) (Detail, error) {
	var d Detail
	if !validID(id) {
		return d, ErrNotFound
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return d, e
	}
	defer tx.Rollback()
	if e = permit(ctx, tx, actor, guard); e != nil {
		return d, e
	}
	e = tx.QueryRowContext(ctx, `SELECT `+columns+`,a.first_name,a.last_name,a.email FROM account_deletion_requests d JOIN accounts a ON a.id=d.account_id WHERE d.id=$1`, id).Scan(&d.ID, &d.AccountID, &d.Status, &d.Version, &d.PolicyVersion, &d.Consent, &d.CreatedAt, &d.UpdatedAt, &d.FirstName, &d.LastName, &d.Email)
	if errors.Is(e, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if e != nil {
		return d, e
	}
	if e = s.audit(ctx, tx, actor, d.Request, "viewed"); e != nil {
		return Detail{}, e
	}
	return d, tx.Commit()
}
func (s *Service) Review(ctx context.Context, actor, id string, version int, guard Guard) (Request, error) {
	if !validID(id) {
		return Request{}, ErrNotFound
	}
	if version < 1 {
		return Request{}, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Request{}, e
	}
	defer tx.Rollback()
	if guard == nil {
		return Request{}, access.ErrForbidden
	}
	if e = access.CheckTx(ctx, tx, actor, "accounts.deletion.review", ""); e != nil {
		return Request{}, e
	}
	r, e := scanRequest(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM account_deletion_requests d WHERE d.id=$1 FOR UPDATE`, id))
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if e = guard(ctx, tx); e != nil {
		return Request{}, e
	}
	if r.Version != version || r.Status == "restored" {
		return Request{}, ErrConflict
	}
	if r.Status == "reviewing" {
		return r, tx.Commit()
	}
	r.Status = "reviewing"
	r.Version++
	r.UpdatedAt = s.now().UTC()
	if _, e = tx.ExecContext(ctx, `UPDATE account_deletion_requests SET status=$2,version=$3,updated_at=$4 WHERE id=$1`, id, r.Status, r.Version, r.UpdatedAt); e != nil {
		return Request{}, e
	}
	if e = s.audit(ctx, tx, actor, r, "reviewing"); e != nil {
		return Request{}, e
	}
	return r, tx.Commit()
}

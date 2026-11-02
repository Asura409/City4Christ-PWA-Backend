package visitors

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Service) List(ctx context.Context, actor string, o Options) (Page, error) {
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Status == "" {
		o.Status = "new"
	}
	if o.Limit < 1 || o.Limit > 100 || (o.Cursor != "" && !validID(o.Cursor)) || (o.Status != "new" && o.Status != "contacted" && o.Status != "closed") {
		return Page{}, ErrInvalid
	}
	tx, e := s.repo.db.BeginTx(ctx, nil)
	if e != nil {
		return Page{}, e
	}
	defer tx.Rollback()
	global, permitErr := permitOrAssigned(ctx, tx, actor, "", s.now())
	if permitErr != nil {
		return Page{}, permitErr
	}
	if s.protection == nil {
		return Page{}, ErrUnavailable
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,sealed_payload,key_id,status,version,created_at,expires_at,COALESCE(assigned_to::text,'') FROM visitor_intakes WHERE sealed_payload IS NOT NULL AND expires_at>$1 AND status=$2 AND ($3='' OR (created_at,id)>(SELECT created_at,id FROM visitor_intakes WHERE id=NULLIF($3,'')::uuid)) AND ($5::boolean OR assigned_to=$6) ORDER BY created_at,id LIMIT $4`, s.now().UTC(), o.Status, o.Cursor, o.Limit+1, global, actor)
	if e != nil {
		return Page{}, e
	}
	items := []Summary{}
	for rows.Next() {
		var v Summary
		var sealed []byte
		var key string
		if e = rows.Scan(&v.ID, &sealed, &key, &v.Status, &v.Version, &v.CreatedAt, &v.ExpiresAt, &v.AssignedTo); e != nil {
			rows.Close()
			return Page{}, e
		}
		c, e := s.protection.open(v.ID, key, sealed)
		if e != nil {
			rows.Close()
			return Page{}, e
		}
		v.FullName = c.FullName
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return Page{}, e
	}
	rows.Close()
	p := Page{Items: items}
	if len(items) > o.Limit {
		p.Items = items[:o.Limit]
		p.NextCursor = p.Items[o.Limit-1].ID
	}
	for _, v := range p.Items {
		if e = audit(ctx, tx, actor, v, "listed", "", s.now()); e != nil {
			return Page{}, e
		}
	}
	return p, tx.Commit()
}
func (s *Service) Detail(ctx context.Context, actor, id string) (Detail, error) {
	if !validID(id) {
		return Detail{}, ErrNotFound
	}
	tx, e := s.repo.db.BeginTx(ctx, nil)
	if e != nil {
		return Detail{}, e
	}
	defer tx.Rollback()
	if _, e = permitOrAssigned(ctx, tx, actor, id, s.now()); e != nil {
		return Detail{}, e
	}
	if s.protection == nil {
		return Detail{}, ErrUnavailable
	}
	var d Detail
	var key string
	var sealed []byte
	e = tx.QueryRowContext(ctx, `SELECT id,status,version,created_at,expires_at,sealed_payload,key_id,consent_version,consent_text,COALESCE(assigned_to::text,'') FROM visitor_intakes WHERE id=$1 AND sealed_payload IS NOT NULL AND expires_at>$2`, id, s.now().UTC()).Scan(&d.ID, &d.Status, &d.Version, &d.CreatedAt, &d.ExpiresAt, &sealed, &key, &d.PolicyVersion, &d.ConsentText, &d.AssignedTo)
	if errors.Is(e, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if e != nil {
		return Detail{}, e
	}
	c, e := s.protection.open(id, key, sealed)
	if e != nil {
		return Detail{}, e
	}
	d.FullName, d.Email, d.Phone = c.FullName, c.Email, c.Phone
	if e = audit(ctx, tx, actor, d.Summary, "viewed", "", s.now()); e != nil {
		return Detail{}, e
	}
	return d, tx.Commit()
}
func (s *Service) Update(ctx context.Context, actor, id string, in Update) (Summary, error) {
	if !validID(id) {
		return Summary{}, ErrNotFound
	}
	if in.Version < 1 || (in.Status != "new" && in.Status != "contacted" && in.Status != "closed" && in.Status != "withdrawn") {
		return Summary{}, ErrInvalid
	}
	tx, e := s.repo.db.BeginTx(ctx, nil)
	if e != nil {
		return Summary{}, e
	}
	defer tx.Rollback()
	var v Summary
	e = tx.QueryRowContext(ctx, `SELECT id,status,version,created_at,expires_at,COALESCE(assigned_to::text,'') FROM visitor_intakes WHERE id=$1 AND sealed_payload IS NOT NULL AND expires_at>$2 FOR UPDATE`, id, s.now().UTC()).Scan(&v.ID, &v.Status, &v.Version, &v.CreatedAt, &v.ExpiresAt, &v.AssignedTo)
	if errors.Is(e, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if e != nil {
		return Summary{}, e
	}
	global, e := permitOrAssigned(ctx, tx, actor, id, s.now())
	if e != nil {
		return Summary{}, e
	}
	if in.Status == "withdrawn" && !global {
		return Summary{}, ErrForbidden
	}
	if v.Version != in.Version {
		return Summary{}, ErrConflict
	}
	if v.Status == in.Status {
		return v, tx.Commit()
	}
	old := v.Status
	v.Status = in.Status
	v.Version++
	_, e = tx.ExecContext(ctx, `UPDATE visitor_intakes SET status=$2,version=version+1,updated_at=$3,sealed_payload=CASE WHEN $2='withdrawn' THEN NULL ELSE sealed_payload END,key_id=CASE WHEN $2='withdrawn' THEN NULL ELSE key_id END,request_digest=CASE WHEN $2='withdrawn' THEN NULL ELSE request_digest END,assigned_to=CASE WHEN $2='withdrawn' THEN NULL ELSE assigned_to END WHERE id=$1`, id, in.Status, s.now().UTC())
	if e != nil {
		return Summary{}, e
	}
	action := "status_changed"
	if in.Status == "withdrawn" {
		action = "withdrawn"
		v.AssignedTo = ""
	}
	if e = audit(ctx, tx, actor, v, action, old, s.now()); e != nil {
		return Summary{}, e
	}
	return v, tx.Commit()
}

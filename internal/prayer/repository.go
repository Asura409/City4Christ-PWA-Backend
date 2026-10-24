package prayer

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("prayer request not found")

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const requestSelect = `
SELECT p.id,p.body,p.visibility,p.group_id,p.anonymous,p.status,
 CASE WHEN p.author_id=$1 THEN 'You'
      WHEN p.anonymous THEN 'Anonymous'
      ELSE trim(a.first_name || ' ' || a.last_name) END,
 p.author_id=$1,p.created_at,p.answered_at,
 (SELECT count(*)::int FROM prayer_acknowledgements pa WHERE pa.request_id=p.id),
 EXISTS(SELECT 1 FROM prayer_acknowledgements pa WHERE pa.request_id=p.id AND pa.account_id=$1)
FROM prayer_requests p JOIN accounts a ON a.id=p.author_id
WHERE p.status<>'archived' AND (
 p.visibility='community' OR
 (p.visibility='private' AND p.author_id=$1) OR
 (p.visibility='group' AND EXISTS(
  SELECT 1 FROM group_memberships gm JOIN groups g ON g.id=gm.group_id
  WHERE gm.group_id=p.group_id AND gm.account_id=$1 AND gm.status='active' AND g.archived_at IS NULL
 ))
)`

func (r *Repository) Create(ctx context.Context, author string, in CreateInput) (Request, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `INSERT INTO prayer_requests(author_id,body,visibility,group_id,anonymous)
 VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5) RETURNING id`, author, in.Body, in.Visibility, in.GroupID, in.Anonymous).Scan(&id)
	if err != nil {
		return Request{}, err
	}
	return r.Get(ctx, author, id)
}

func (r *Repository) IsActiveGroupMember(ctx context.Context, actor, groupID string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM group_memberships gm JOIN groups g ON g.id=gm.group_id
 WHERE gm.group_id=$1 AND gm.account_id=$2 AND gm.status='active' AND g.archived_at IS NULL
)`, groupID, actor).Scan(&active)
	return active, err
}

func (r *Repository) List(ctx context.Context, actor string, o Options, after *Cursor) ([]Request, error) {
	var createdAt *time.Time
	var cursorID *string
	if after != nil {
		createdAt = &after.CreatedAt
		cursorID = &after.ID
	}
	query := requestSelect + ` AND ($2='' OR p.status=$2)
 AND ($3::timestamptz IS NULL OR (p.created_at,p.id)<($3::timestamptz,$4::uuid))
 ORDER BY p.created_at DESC,p.id DESC LIMIT $5`
	rows, err := r.db.QueryContext(ctx, query, actor, o.Status, createdAt, cursorID, o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Request, 0)
	for rows.Next() {
		item, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, actor, requestID string) (Request, error) {
	item, err := scanRequest(r.db.QueryRowContext(ctx, requestSelect+` AND p.id=$2`, actor, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) SetStatus(ctx context.Context, actor, requestID, status string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT p.status FROM prayer_requests p WHERE p.id=$1 AND p.author_id=$2 AND (
	 p.visibility<>'group' OR EXISTS(
	  SELECT 1 FROM group_memberships gm JOIN groups g ON g.id=gm.group_id
	  WHERE gm.group_id=p.group_id AND gm.account_id=$2 AND gm.status='active' AND g.archived_at IS NULL
	 )
	) FOR UPDATE`, requestID, actor).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	if current == status {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if current == "archived" && status == "answered" {
		return false, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE prayer_requests
 SET status=$2,updated_at=now(),
     answered_at=CASE WHEN $2='answered' THEN COALESCE(answered_at,now()) ELSE answered_at END,
     archived_at=CASE WHEN $2='archived' THEN COALESCE(archived_at,now()) ELSE NULL END
 WHERE id=$1`, requestID, status); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) Pray(ctx context.Context, actor, requestID string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var visible string
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM prayer_requests p
 WHERE p.id=$1 AND p.status='open' AND p.archived_at IS NULL AND (
  p.visibility='community' OR (p.visibility='private' AND p.author_id=$2) OR
  (p.visibility='group' AND EXISTS(
   SELECT 1 FROM group_memberships gm JOIN groups g ON g.id=gm.group_id
   WHERE gm.group_id=p.group_id AND gm.account_id=$2 AND gm.status='active' AND g.archived_at IS NULL
  ))
 ) FOR UPDATE`, requestID, actor).Scan(&visible)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO prayer_acknowledgements(request_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, requestID, actor)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed == 1, nil
}

func scanRequest(row interface{ Scan(...any) error }) (Request, error) {
	var item Request
	var groupID sql.NullString
	var answeredAt sql.NullTime
	err := row.Scan(&item.ID, &item.Body, &item.Visibility, &groupID, &item.Anonymous,
		&item.Status, &item.Author, &item.IsOwner, &item.CreatedAt, &answeredAt,
		&item.PrayerCount, &item.HasPrayed)
	if err != nil {
		return Request{}, err
	}
	if groupID.Valid {
		item.GroupID = groupID.String
	}
	item.CreatedAt = item.CreatedAt.UTC()
	if answeredAt.Valid {
		answered := answeredAt.Time.UTC()
		item.AnsweredAt = &answered
	}
	return item, nil
}

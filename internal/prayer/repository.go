package prayer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
 p.author_id=$1,p.created_at,p.expires_at,p.answered_at,
 (SELECT count(*)::int FROM prayer_acknowledgements pa WHERE pa.request_id=p.id),
 EXISTS(SELECT 1 FROM prayer_acknowledgements pa WHERE pa.request_id=p.id AND pa.account_id=$1)
FROM prayer_requests p JOIN accounts a ON a.id=p.author_id
WHERE p.status<>'archived' AND p.expires_at>now() AND (
 p.visibility='community' OR
 (p.visibility='private' AND p.author_id=$1) OR
 (p.visibility='group' AND EXISTS(
  SELECT 1 FROM group_memberships gm JOIN groups g ON g.id=gm.group_id
  WHERE gm.group_id=p.group_id AND gm.account_id=$1 AND gm.status='active' AND g.archived_at IS NULL
 ))
)`

func (r *Repository) Create(ctx context.Context, author string, in CreateInput, now time.Time) (Request, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `INSERT INTO prayer_requests(author_id,body,visibility,group_id,anonymous,created_at,updated_at,expires_at)
 VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$6,$7) RETURNING id`, author, in.Body, in.Visibility, in.GroupID, in.Anonymous, now, now.Add(requestLifetime)).Scan(&id)
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

// Expire archives a bounded batch and closes pending reports without deleting
// the underlying prayer text. Repeated calls are safe and concurrent callers
// skip rows held by another transaction.
func (r *Repository) Expire(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 5000 {
		return 0, fmt.Errorf("invalid prayer expiry batch size")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM prayer_requests
 WHERE status<>'archived' AND expires_at<=$1 ORDER BY expires_at,id LIMIT $2 FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE prayer_requests SET status='archived',archive_reason='expiry',archived_at=$2,updated_at=$2 WHERE id=$1`, id, now); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE prayer_reports SET status='expired',closed_at=$2
 WHERE request_id=$1 AND status='pending'`, id, now); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO prayer_moderation_events(request_id,action,reason,occurred_at)
 VALUES($1,'expired','Request retention period elapsed.',$2)`, id, now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (r *Repository) Report(ctx context.Context, actor, requestID string, input ReportInput) (Report, error) {
	var owner bool
	err := r.db.QueryRowContext(ctx, `SELECT author_id=$2 FROM prayer_requests
 WHERE id=$1 AND status<>'archived' AND expires_at>now() AND visibility<>'private'
 AND (visibility='community' OR EXISTS(SELECT 1 FROM group_memberships gm JOIN groups g ON g.id=gm.group_id
  WHERE gm.group_id=prayer_requests.group_id AND gm.account_id=$2 AND gm.status='active' AND g.archived_at IS NULL))`, requestID, actor).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, err
	}
	if owner {
		return Report{}, ErrConflict
	}
	var report Report
	err = r.db.QueryRowContext(ctx, `INSERT INTO prayer_reports(request_id,reporter_id,reason,details)
 VALUES($1,$2,$3,$4) RETURNING id,request_id,reason,details,status,created_at`, requestID, actor, input.Reason, input.Details).
		Scan(&report.ID, &report.RequestID, &report.Reason, &report.Details, &report.Status, &report.CreatedAt)
	if err != nil {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && state.SQLState() == "23505" {
			return Report{}, ErrConflict
		}
		return Report{}, err
	}
	report.CreatedAt = report.CreatedAt.UTC()
	return report, nil
}

func (r *Repository) ListReports(ctx context.Context, actor string, options Options) ([]Report, error) {
	var allowed bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM account_roles ar JOIN accounts a ON a.id=ar.account_id
 WHERE ar.account_id=$1 AND a.email_verified_at IS NOT NULL AND ar.scope=''
 AND ar.role IN ('super_administrator','assistant_pastor','staff')) OR EXISTS(
 SELECT 1 FROM account_roles ar JOIN accounts a ON a.id=ar.account_id
 JOIN group_memberships gm ON gm.group_id::text=ar.scope AND gm.account_id=ar.account_id AND gm.status='active'
 JOIN groups g ON g.id=gm.group_id AND g.archived_at IS NULL
 WHERE ar.account_id=$1 AND a.email_verified_at IS NOT NULL AND ar.role='ministry_leader')`, actor).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	rows, err := r.db.QueryContext(ctx, `SELECT pr.id,pr.request_id,p.body,p.visibility,p.group_id,p.anonymous,pr.reason,pr.details,pr.status,pr.created_at
 FROM prayer_reports pr JOIN prayer_requests p ON p.id=pr.request_id
 WHERE pr.status=$2 AND p.status<>'archived' AND p.expires_at>now() AND p.visibility<>'private'
 AND ((p.visibility='community' AND EXISTS(SELECT 1 FROM account_roles ar WHERE ar.account_id=$1 AND ar.scope='' AND ar.role IN ('super_administrator','assistant_pastor','staff')))
  OR (p.visibility='group' AND ((EXISTS(SELECT 1 FROM account_roles ar WHERE ar.account_id=$1 AND ar.role='super_administrator' AND ar.scope='') OR EXISTS(
   SELECT 1 FROM account_roles ar JOIN group_memberships gm ON gm.group_id::text=ar.scope AND gm.account_id=$1 AND gm.status='active'
   JOIN groups g ON g.id=gm.group_id AND g.archived_at IS NULL
   WHERE ar.account_id=$1 AND ar.role='ministry_leader' AND ar.scope=p.group_id::text)))))
 ORDER BY pr.created_at,pr.id LIMIT $3`, actor, options.Status, options.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Report, 0)
	for rows.Next() {
		var item Report
		var groupID sql.NullString
		if err := rows.Scan(&item.ID, &item.RequestID, &item.RequestBody, &item.RequestVisibility, &groupID, &item.Anonymous, &item.Reason, &item.Details, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		if groupID.Valid {
			item.GroupID = groupID.String
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ReviewReport(ctx context.Context, actor, reportID string, input ReviewInput, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var requestID, visibility string
	var groupID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.visibility,p.group_id FROM prayer_reports pr
 JOIN prayer_requests p ON p.id=pr.request_id
 WHERE pr.id=$1 AND pr.status='pending' AND p.status<>'archived' AND p.expires_at>$2 AND p.visibility<>'private'
 FOR UPDATE OF pr,p`, reportID, now).Scan(&requestID, &visibility, &groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	var allowed bool
	scope := ""
	if groupID.Valid {
		scope = groupID.String
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM account_roles ar JOIN accounts a ON a.id=ar.account_id
 WHERE ar.account_id=$1 AND a.email_verified_at IS NOT NULL AND ar.scope='' AND (
  ($2='community' AND ar.role IN ('super_administrator','assistant_pastor','staff')) OR
  ($2='group' AND ar.role='super_administrator')))
 OR ($2='group' AND EXISTS(
  SELECT 1 FROM account_roles ar JOIN accounts a ON a.id=ar.account_id
  JOIN group_memberships gm ON gm.group_id::text=$3 AND gm.account_id=$1 AND gm.status='active'
  JOIN groups g ON g.id=gm.group_id AND g.archived_at IS NULL
  WHERE ar.account_id=$1 AND a.email_verified_at IS NOT NULL AND ar.role='ministry_leader' AND ar.scope=$3))`, actor, visibility, scope).Scan(&allowed); err != nil {
		return false, err
	}
	if !allowed {
		return false, ErrNotFound
	}
	status := "dismissed"
	action := "dismissed"
	if input.Decision == "hide" {
		status = "resolved"
		action = "hidden"
		if _, err = tx.ExecContext(ctx, `UPDATE prayer_requests SET status='archived',archive_reason='moderation',archived_at=$2,updated_at=$2 WHERE id=$1`, requestID, now); err != nil {
			return false, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE prayer_reports SET status=$2,closed_at=$3,closed_by=$4 WHERE id=$1 AND status='pending'`, reportID, status, now, actor)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		if err != nil {
			return false, err
		}
		return false, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO prayer_moderation_events(request_id,report_id,actor_id,action,reason,occurred_at)
 VALUES($1,$2,$3,$4,$5,$6)`, requestID, reportID, actor, action, input.Reason, now); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
		 archived_at=CASE WHEN $2='archived' THEN COALESCE(archived_at,now()) ELSE NULL END,
		 archive_reason=CASE WHEN $2='archived' THEN 'owner' ELSE NULL END
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
	WHERE p.id=$1 AND p.status='open' AND p.archived_at IS NULL AND p.expires_at>now() AND (
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
	var expiresAt time.Time
	err := row.Scan(&item.ID, &item.Body, &item.Visibility, &groupID, &item.Anonymous,
		&item.Status, &item.Author, &item.IsOwner, &item.CreatedAt, &expiresAt, &answeredAt,
		&item.PrayerCount, &item.HasPrayed)
	if err != nil {
		return Request{}, err
	}
	if groupID.Valid {
		item.GroupID = groupID.String
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ExpiresAt = expiresAt.UTC()
	if answeredAt.Valid {
		answered := answeredAt.Time.UTC()
		item.AnsweredAt = &answered
	}
	return item, nil
}

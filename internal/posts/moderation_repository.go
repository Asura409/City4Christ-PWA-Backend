package posts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

func (r *ModerationRepository) Report(ctx context.Context, actor string, input ReportInput, now time.Time) (Report, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()
	var authorID, postID, body string
	if input.TargetType == "post" {
		err = tx.QueryRowContext(ctx, `SELECT author_id::text,id::text,body FROM church_posts WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, input.TargetID).Scan(&authorID, &postID, &body)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT c.account_id::text,c.post_id::text,c.body FROM post_comments c JOIN church_posts p ON p.id=c.post_id
		WHERE c.id=$1 AND c.deleted_at IS NULL AND p.deleted_at IS NULL FOR SHARE OF c,p`, input.TargetID).Scan(&authorID, &postID, &body)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrReportNotFound
	}
	if err != nil {
		return Report{}, err
	}
	if authorID == actor {
		return Report{}, ErrReportConflict
	}
	var item Report
	err = tx.QueryRowContext(ctx, `INSERT INTO post_content_reports(reporter_id,target_type,target_id,post_id,content_snapshot,reason,details,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text,target_type,target_id::text,post_id::text,content_snapshot,reason,details,status,created_at`,
		actor, input.TargetType, input.TargetID, postID, body, input.Reason, input.Details, now).
		Scan(&item.ID, &item.TargetType, &item.TargetID, &item.PostID, &item.Content, &item.Reason, &item.Details, &item.Status, &item.CreatedAt)
	if err != nil {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && state.SQLState() == "23505" {
			return Report{}, ErrReportConflict
		}
		return Report{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	if err := tx.Commit(); err != nil {
		return Report{}, err
	}
	return item, nil
}

func (r *ModerationRepository) List(ctx context.Context, actor string, options ReportOptions, after *reportCursor) ([]Report, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = access.CheckTx(ctx, tx, actor, "groups.content.moderate", ""); errors.Is(err, access.ErrForbidden) {
		return nil, ErrModerationForbidden
	} else if err != nil {
		return nil, err
	}
	var createdAt *time.Time
	var cursorID *string
	if after != nil {
		createdAt, cursorID = &after.CreatedAt, &after.ID
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,target_type,target_id::text,post_id::text,content_snapshot,reason,details,status,created_at
		FROM post_content_reports WHERE status=$1 AND ($2::timestamptz IS NULL OR (created_at,id)>($2::timestamptz,$3::uuid))
		ORDER BY created_at,id LIMIT $4`, options.Status, createdAt, cursorID, options.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Report, 0)
	for rows.Next() {
		var item Report
		if err := rows.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.PostID, &item.Content, &item.Reason, &item.Details, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ModerationRepository) Review(ctx context.Context, permissions *access.Service, actor, reportID string, input ReviewInput, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = access.CheckTx(ctx, tx, actor, "groups.content.moderate", ""); errors.Is(err, access.ErrForbidden) {
		return ErrModerationForbidden
	} else if err != nil {
		return err
	}
	var targetType, targetID, postID, status string
	err = tx.QueryRowContext(ctx, `SELECT target_type,target_id::text,post_id::text,status FROM post_content_reports WHERE id=$1`, reportID).Scan(&targetType, &targetID, &postID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReportNotFound
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrReportConflict
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('post-moderation/' || $1,0))`, postID); err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT status FROM post_content_reports WHERE id=$1 FOR UPDATE`, reportID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReportNotFound
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrReportConflict
	}
	if input.Decision == "dismiss" {
		if err := closeReport(ctx, tx, reportID, actor, targetType, targetID, "dismissed", input.Reason, now); err != nil {
			return err
		}
	} else {
		if err := hideReportedContent(ctx, tx, targetType, targetID, postID, now); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text,target_type,target_id::text FROM post_content_reports
			WHERE post_id=$1 AND status='pending' ORDER BY id FOR UPDATE`, postID)
		if err != nil {
			return err
		}
		type targetReport struct{ id, kind, target string }
		pending := make([]targetReport, 0)
		for rows.Next() {
			var item targetReport
			if err := rows.Scan(&item.id, &item.kind, &item.target); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, item := range pending {
			reason := input.Reason
			if item.id != reportID {
				reason = "Related content hidden during review."
			}
			if err := closeReport(ctx, tx, item.id, actor, item.kind, item.target, "resolved", reason, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func hideReportedContent(ctx context.Context, tx *sql.Tx, targetType, targetID, postID string, now time.Time) error {
	if targetType == "post" {
		if _, err := tx.ExecContext(ctx, `UPDATE church_posts SET body='[Post removed by moderation]',deleted_at=COALESCE(deleted_at,$2) WHERE id=$1`, postID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE post_comments SET body='[Comment removed by moderation]',deleted_at=COALESCE(deleted_at,$2) WHERE post_id=$1`, postID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM post_reactions WHERE post_id=$1`, postID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM post_media_attachments WHERE post_id=$1`, postID)
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE post_comments SET body='[Comment removed by moderation]',deleted_at=COALESCE(deleted_at,$3)
		WHERE id=$1 AND post_id=$2`, targetID, postID, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrReportNotFound
	}
	return nil
}

func closeReport(ctx context.Context, tx *sql.Tx, reportID, actor, targetType, targetID, status, reason string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE post_content_reports SET status=$2,closed_at=$3,closed_by=$4 WHERE id=$1 AND status='pending'`, reportID, status, now, actor)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrReportConflict
	}
	action := "dismissed"
	if status == "resolved" {
		action = "hidden"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO post_moderation_events(report_id,actor_id,target_type,target_id,action,reason,occurred_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, reportID, actor, targetType, targetID, action, reason, now)
	return err
}

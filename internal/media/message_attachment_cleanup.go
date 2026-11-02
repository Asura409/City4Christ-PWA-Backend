package media

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// DetachDirectMessageAttachmentsTx removes a message's links and queues each
// now-unreferenced private R2 object for durable worker cleanup.
func DetachDirectMessageAttachmentsTx(ctx context.Context, tx *sql.Tx, messageID string) error {
	return detachMessageAttachmentsTx(ctx, tx, "direct_message_media_attachments", messageID)
}

// DetachGroupMessageAttachmentsTx removes a message's links and queues each
// now-unreferenced private R2 object for durable worker cleanup.
func DetachGroupMessageAttachmentsTx(ctx context.Context, tx *sql.Tx, messageID string) error {
	return detachMessageAttachmentsTx(ctx, tx, "group_chat_message_media_attachments", messageID)
}

func detachMessageAttachmentsTx(ctx context.Context, tx *sql.Tx, table, messageID string) error {
	if table != "direct_message_media_attachments" && table != "group_chat_message_media_attachments" {
		return ErrInvalidAttachment
	}
	rows, err := tx.QueryContext(ctx, "SELECT u.id::text,u.available_object_key FROM "+table+" a JOIN media_uploads u ON u.id=a.upload_id WHERE a.message_id=$1 AND u.status='available' FOR UPDATE OF u", messageID)
	if err != nil {
		return err
	}
	type object struct{ id, key string }
	objects := make([]object, 0)
	for rows.Next() {
		var item object
		if err := rows.Scan(&item.id, &item.key); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE message_id=$1", messageID); err != nil {
		return err
	}
	for _, item := range objects {
		var references int
		if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT count(*) FROM direct_message_media_attachments WHERE upload_id=$1)+
			(SELECT count(*) FROM group_chat_message_media_attachments WHERE upload_id=$1)`, item.id).Scan(&references); err != nil {
			return err
		}
		if references != 0 {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status='deleting' WHERE id=$1 AND status='available' AND available_object_key=$2`, item.id, item.key)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_object_cleanup_jobs(id,upload_id,object_key) VALUES($1,$2,$3) ON CONFLICT(upload_id) DO NOTHING`, uuid.NewString(), item.id, item.key); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ClaimObjectCleanup(ctx context.Context, leaseDuration time.Duration) (ObjectCleanupJob, error) {
	var job ObjectCleanupJob
	if leaseDuration <= 0 {
		leaseDuration = defaultScanLease
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id::text,upload_id::text,object_key,status FROM media_object_cleanup_jobs
		WHERE (status='queued' AND available_at<=now()) OR (status='leased' AND lease_expires_at<=now())
		ORDER BY available_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&job.ID, &job.UploadID, &job.ObjectKey, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ObjectCleanupJob{}, ErrNoCleanupJobs
	}
	if err != nil {
		return ObjectCleanupJob{}, err
	}
	job.LeaseToken = uuid.NewString()
	if err := tx.QueryRowContext(ctx, `UPDATE media_object_cleanup_jobs SET status='leased',attempts=attempts+1,lease_token=$2,
		lease_expires_at=now()+($3*interval '1 second'),updated_at=now() WHERE id=$1 RETURNING attempts`, job.ID, job.LeaseToken, leaseDuration.Seconds()).Scan(&job.Attempts); err != nil {
		return ObjectCleanupJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return ObjectCleanupJob{}, err
	}
	return job, nil
}

func (r *Repository) CompleteObjectCleanup(ctx context.Context, jobID, leaseToken string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var uploadID string
	err = tx.QueryRowContext(ctx, `SELECT upload_id::text FROM media_object_cleanup_jobs WHERE id=$1 AND status='leased'
		AND lease_token=$2 AND lease_expires_at>now() FOR UPDATE`, jobID, leaseToken).Scan(&uploadID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCleanupLeaseLost
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status='deleted',available_object_key=NULL WHERE id=$1 AND status='deleting'`, uploadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_object_cleanup_jobs WHERE id=$1`, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) FailObjectCleanup(ctx context.Context, jobID, leaseToken, errorCode string, retryAfter time.Duration) error {
	if errorCode != "storage_unavailable" {
		errorCode = "storage_unavailable"
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	result, err := r.db.ExecContext(ctx, `UPDATE media_object_cleanup_jobs SET
		status=CASE WHEN attempts>=8 THEN 'dead' ELSE 'queued' END,
		available_at=now()+($4*interval '1 second'),lease_token=NULL,lease_expires_at=NULL,
		last_error_code=$3,updated_at=now() WHERE id=$1 AND status='leased' AND lease_token=$2`, jobID, leaseToken, errorCode, retryAfter.Seconds())
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrCleanupLeaseLost
	}
	return nil
}

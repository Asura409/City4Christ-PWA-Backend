package media

import (
	"context"
	"database/sql"
	"errors"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	PurposePostAttachment   UploadPurpose = "post_attachment"
	PurposeSermonAttachment UploadPurpose = "sermon_attachment"
	UploadPending                         = "pending_upload"
	UploadScanQueued                      = "scan_queued"
	UploadScanning                        = "scanning"
	UploadAvailable                       = "available"
	UploadRejected                        = "rejected"
	UploadScanFailed                      = "scan_failed"
	UploadExpired                         = "expired"
	maxScanAttempts                       = 5
)

var (
	ErrInvalidMediaUpload = errors.New("invalid media upload")
	ErrUploadMismatch     = errors.New("uploaded object does not match its reservation")
	ErrUploadNotFound     = errors.New("media upload not found")
	ErrUploadExpired      = errors.New("media upload reservation expired")
	ErrNoScanJobs         = errors.New("no media scan jobs available")
	ErrScanLeaseLost      = errors.New("media scan lease lost")
	ErrInvalidScanResult  = errors.New("invalid media scan result")
)

type UploadPurpose string

type Upload struct {
	ID                  string
	OwnerID             string
	Purpose             UploadPurpose
	ObjectKey           string
	AvailableObjectKey  sql.NullString
	DeclaredContentType string
	DeclaredSize        int64
	Status              string
	ActualContentType   sql.NullString
	ActualSize          sql.NullInt64
	CreatedAt           time.Time
	ExpiresAt           time.Time
	UploadedAt          sql.NullTime
	ScannedAt           sql.NullTime
}

type CreateUploadInput struct {
	OwnerID             string
	Purpose             UploadPurpose
	ObjectKey           string
	DeclaredContentType string
	DeclaredSize        int64
	ExpiresAt           time.Time
}

type ScanJob struct {
	ID             string
	UploadID       string
	Purpose        UploadPurpose
	ObjectKey      string
	ContentType    string
	Size           int64
	Attempts       int
	LeaseToken     string
	LeaseExpiresAt time.Time
}

type ScanResult struct {
	Clean              bool
	AvailableObjectKey string
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ByOwner(ctx context.Context, uploadID, ownerID string) (Upload, error) {
	if uuid.Validate(uploadID) != nil || uuid.Validate(ownerID) != nil {
		return Upload{}, ErrUploadNotFound
	}
	upload, err := scanUpload(r.db.QueryRowContext(ctx, `SELECT id::text,owner_id::text,purpose,object_key,available_object_key,declared_content_type,
		declared_size,status,actual_content_type,actual_size,created_at,expires_at,uploaded_at,scanned_at
		FROM media_uploads WHERE id=$1 AND owner_id=$2`, uploadID, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return Upload{}, ErrUploadNotFound
	}
	return upload, err
}

func (r *Repository) ExpirePending(ctx context.Context, uploadID, ownerID string) (bool, error) {
	if uuid.Validate(uploadID) != nil || uuid.Validate(ownerID) != nil {
		return false, ErrUploadNotFound
	}
	result, err := r.db.ExecContext(ctx, `UPDATE media_uploads SET status='expired'
		WHERE id=$1 AND owner_id=$2 AND status='pending_upload' AND expires_at<=now()`, uploadID, ownerID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (r *Repository) CreateUpload(ctx context.Context, input CreateUploadInput) (Upload, error) {
	contentType, _, err := mime.ParseMediaType(input.DeclaredContentType)
	keyPrefix := ""
	switch input.Purpose {
	case PurposePostAttachment:
		keyPrefix = "posts/"
	case PurposeSermonAttachment:
		keyPrefix = "sermons/"
	}
	if err != nil || !validUploadContentType(input.DeclaredContentType) ||
		(input.Purpose != PurposePostAttachment && input.Purpose != PurposeSermonAttachment) ||
		!validObjectKey(input.ObjectKey) || !strings.HasPrefix(input.ObjectKey, keyPrefix) ||
		input.DeclaredSize <= 0 || uuid.Validate(input.OwnerID) != nil || input.ExpiresAt.IsZero() {
		return Upload{}, ErrInvalidMediaUpload
	}
	contentType = strings.ToLower(contentType)
	id := uuid.NewString()
	return scanUpload(r.db.QueryRowContext(ctx, `
		INSERT INTO media_uploads(id,owner_id,purpose,object_key,declared_content_type,declared_size,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id::text,owner_id::text,purpose,object_key,available_object_key,declared_content_type,declared_size,status,
		 actual_content_type,actual_size,created_at,expires_at,uploaded_at,scanned_at`,
		id, input.OwnerID, input.Purpose, input.ObjectKey, contentType, input.DeclaredSize, input.ExpiresAt))
}

func (r *Repository) QueueScan(ctx context.Context, uploadID, actualContentType string, actualSize int64) (ScanJob, error) {
	mediaType, _, parseErr := mime.ParseMediaType(actualContentType)
	if parseErr == nil {
		mediaType = strings.ToLower(mediaType)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ScanJob{}, err
	}
	defer tx.Rollback()
	var ownerType, key, declaredType, status string
	var declaredSize int64
	var expired bool
	err = tx.QueryRowContext(ctx, `SELECT purpose,object_key,declared_content_type,declared_size,status,expires_at<=now()
		FROM media_uploads WHERE id=$1 FOR UPDATE`, uploadID).Scan(&ownerType, &key, &declaredType, &declaredSize, &status, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return ScanJob{}, ErrUploadNotFound
	}
	if err != nil {
		return ScanJob{}, err
	}
	if status == UploadPending && expired {
		if _, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status='expired' WHERE id=$1`, uploadID); err != nil {
			return ScanJob{}, err
		}
		if err := tx.Commit(); err != nil {
			return ScanJob{}, err
		}
		return ScanJob{}, ErrUploadExpired
	}
	if status != UploadPending {
		job, getErr := scanJob(tx.QueryRowContext(ctx, `SELECT j.id::text,j.upload_id::text,$2,$3,u.actual_size,j.attempts
			FROM media_scan_jobs j JOIN media_uploads u ON u.id=j.upload_id
			WHERE j.upload_id=$1`, uploadID, key, declaredType))
		if getErr == nil {
			if err := tx.Commit(); err != nil {
				return ScanJob{}, err
			}
			return job, nil
		}
		return ScanJob{}, ErrInvalidMediaUpload
	}
	if parseErr != nil || len(mediaType) > 127 || !validUploadContentType(mediaType) || mediaType != declaredType || actualSize <= 0 || actualSize != declaredSize {
		var recordedType any
		var recordedSize any
		if parseErr == nil && len(mediaType) <= 127 && actualSize > 0 {
			recordedType, recordedSize = mediaType, actualSize
		}
		if _, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status='rejected',actual_content_type=$2,actual_size=$3,uploaded_at=now() WHERE id=$1`, uploadID, recordedType, recordedSize); err != nil {
			return ScanJob{}, err
		}
		if err := tx.Commit(); err != nil {
			return ScanJob{}, err
		}
		return ScanJob{}, ErrUploadMismatch
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status='scan_queued',actual_content_type=$2,actual_size=$3,uploaded_at=now() WHERE id=$1`, uploadID, mediaType, actualSize); err != nil {
		return ScanJob{}, err
	}
	jobID := uuid.NewString()
	job, err := scanJob(tx.QueryRowContext(ctx, `
		INSERT INTO media_scan_jobs(id,upload_id) VALUES($1,$2)
		RETURNING id::text,upload_id::text,$3::text,$4::text,$5::bigint,attempts`, jobID, uploadID, key, mediaType, actualSize))
	if err != nil {
		return ScanJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScanJob{}, err
	}
	return job, nil
}

func (r *Repository) ClaimScan(ctx context.Context, leaseDuration time.Duration) (ScanJob, error) {
	if leaseDuration < time.Second || leaseDuration > time.Hour {
		return ScanJob{}, ErrInvalidMediaUpload
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ScanJob{}, err
	}
	defer tx.Rollback()
	var job ScanJob
	err = tx.QueryRowContext(ctx, `
		SELECT j.id::text,j.upload_id::text,u.purpose,u.object_key,u.actual_content_type,u.actual_size,j.attempts
		FROM media_scan_jobs j JOIN media_uploads u ON u.id=j.upload_id
		WHERE (j.status='queued' AND j.available_at<=now())
		   OR (j.status='leased' AND j.lease_expires_at<=now())
		ORDER BY j.available_at,j.created_at,j.id
		LIMIT 1 FOR UPDATE OF j SKIP LOCKED`).Scan(&job.ID, &job.UploadID, &job.Purpose, &job.ObjectKey, &job.ContentType, &job.Size, &job.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return ScanJob{}, ErrNoScanJobs
	}
	if err != nil {
		return ScanJob{}, err
	}
	job.LeaseToken = uuid.NewString()
	err = tx.QueryRowContext(ctx, `UPDATE media_scan_jobs SET status='leased',attempts=attempts+1,lease_token=$2,
		lease_expires_at=now()+($3 * interval '1 second'),updated_at=now() WHERE id=$1 RETURNING lease_expires_at`,
		job.ID, job.LeaseToken, leaseDuration.Seconds()).Scan(&job.LeaseExpiresAt)
	if err != nil {
		return ScanJob{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status='scanning' WHERE id=$1 AND status IN ('scan_queued','scanning')`, job.UploadID)
	if err != nil {
		return ScanJob{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ScanJob{}, ErrInvalidMediaUpload
	}
	job.Attempts++
	if err := tx.Commit(); err != nil {
		return ScanJob{}, err
	}
	return job, nil
}

func (r *Repository) CompleteScan(ctx context.Context, jobID, leaseToken string, scan ScanResult) error {
	if scan.Clean && (!validObjectKey(scan.AvailableObjectKey) || !strings.HasPrefix(scan.AvailableObjectKey, "available/")) {
		return ErrInvalidScanResult
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var uploadID string
	err = tx.QueryRowContext(ctx, `UPDATE media_scan_jobs SET status='completed',lease_token=NULL,lease_expires_at=NULL,finished_at=now(),updated_at=now(),last_error_code=NULL
		WHERE id=$1 AND status='leased' AND lease_token=$2 AND lease_expires_at>now() RETURNING upload_id::text`, jobID, leaseToken).Scan(&uploadID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrScanLeaseLost
	}
	if err != nil {
		return err
	}
	status := UploadAvailable
	if !scan.Clean {
		status = UploadRejected
	}
	result, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status=$2,available_object_key=CASE WHEN $3 THEN $4 ELSE NULL END,scanned_at=now() WHERE id=$1 AND status='scanning'`, uploadID, status, scan.Clean, nullableKey(scan.AvailableObjectKey))
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrScanLeaseLost
	}
	return tx.Commit()
}

func nullableKey(key string) any {
	if key == "" {
		return nil
	}
	return key
}

func (r *Repository) FailScan(ctx context.Context, jobID, leaseToken, errorCode string, retryAfter time.Duration) error {
	if (errorCode != "scanner_unavailable" && errorCode != "storage_unavailable") || retryAfter < 0 || retryAfter > 24*time.Hour {
		return ErrInvalidScanResult
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var uploadID string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT upload_id::text,attempts FROM media_scan_jobs WHERE id=$1 AND status='leased' AND lease_token=$2 AND lease_expires_at>now() FOR UPDATE`, jobID, leaseToken).Scan(&uploadID, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrScanLeaseLost
	}
	if err != nil {
		return err
	}
	jobStatus, uploadStatus := "queued", UploadScanQueued
	var finished any
	if attempts >= maxScanAttempts {
		jobStatus, uploadStatus, finished = "dead", UploadScanFailed, time.Now().UTC()
	}
	_, err = tx.ExecContext(ctx, `UPDATE media_scan_jobs SET status=$2,available_at=now()+($3 * interval '1 second'),lease_token=NULL,lease_expires_at=NULL,last_error_code=$4,finished_at=$5,updated_at=now() WHERE id=$1`, jobID, jobStatus, retryAfter.Seconds(), errorCode, finished)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE media_uploads SET status=$2 WHERE id=$1 AND status='scanning'`, uploadID, uploadStatus)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrInvalidMediaUpload
	}
	return tx.Commit()
}

func scanUpload(row *sql.Row) (Upload, error) {
	var upload Upload
	err := row.Scan(&upload.ID, &upload.OwnerID, &upload.Purpose, &upload.ObjectKey, &upload.AvailableObjectKey, &upload.DeclaredContentType,
		&upload.DeclaredSize, &upload.Status, &upload.ActualContentType, &upload.ActualSize, &upload.CreatedAt, &upload.ExpiresAt, &upload.UploadedAt, &upload.ScannedAt)
	return upload, err
}

func scanJob(row *sql.Row) (ScanJob, error) {
	var job ScanJob
	err := row.Scan(&job.ID, &job.UploadID, &job.ObjectKey, &job.ContentType, &job.Size, &job.Attempts)
	return job, err
}

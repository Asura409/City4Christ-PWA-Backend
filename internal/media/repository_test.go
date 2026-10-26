package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestMediaRepositoryPersistsUploadsAndQueuesScansIdempotently(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	upload := validUpload(owner)

	created, err := repo.CreateUpload(ctx, upload)
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if created.Status != UploadPending || created.OwnerID != owner || created.DeclaredSize != upload.DeclaredSize {
		t.Fatalf("created upload = %#v", created)
	}
	var jobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("scan jobs before upload completion = %d, err %v; want zero", jobs, err)
	}

	first, err := repo.QueueScan(ctx, created.ID, "image/png", upload.DeclaredSize)
	if err != nil {
		t.Fatalf("QueueScan() error = %v", err)
	}
	second, err := repo.QueueScan(ctx, created.ID, "image/png", upload.DeclaredSize)
	if err != nil {
		t.Fatalf("idempotent QueueScan() error = %v", err)
	}
	if first.ID != second.ID || first.UploadID != second.UploadID {
		t.Fatalf("QueueScan() jobs differ: first %#v, second %#v", first, second)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs WHERE upload_id=$1`, created.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("scan jobs after completion = %d, err %v; want one", jobs, err)
	}
}

func TestMediaRepositoryRejectsObjectMismatchBeforeQueueing(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	upload, err := repo.CreateUpload(ctx, validUpload(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.QueueScan(ctx, upload.ID, "image/jpeg", upload.DeclaredSize); !errors.Is(err, ErrUploadMismatch) {
		t.Fatalf("QueueScan() error = %v, want ErrUploadMismatch", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM media_uploads WHERE id=$1`, upload.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(UploadRejected) {
		t.Fatalf("mismatched upload status = %q, want rejected", status)
	}
	var jobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs WHERE upload_id=$1`, upload.ID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("mismatched upload jobs = %d, err %v; want zero", jobs, err)
	}
}

func TestMediaRepositoryExpiresReservationsWithoutEnqueueing(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	upload, err := repo.CreateUpload(ctx, validUpload(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE media_uploads SET expires_at=created_at+interval '1 second' WHERE id=$1`, upload.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := repo.QueueScan(ctx, upload.ID, "image/png", upload.DeclaredSize); !errors.Is(err, ErrUploadExpired) {
		t.Fatalf("QueueScan() error = %v, want ErrUploadExpired", err)
	}
	var status string
	var jobs int
	if err := db.QueryRowContext(ctx, `SELECT status FROM media_uploads WHERE id=$1`, upload.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs WHERE upload_id=$1`, upload.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if status != UploadExpired || jobs != 0 {
		t.Fatalf("expired upload status=%q jobs=%d; want expired and zero jobs", status, jobs)
	}
}

func TestMediaRepositoryClaimsWithLeaseAndOnlyCurrentWorkerCanFinish(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	upload, err := repo.CreateUpload(ctx, validUpload(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.QueueScan(ctx, upload.ID, "image/png", upload.DeclaredSize); err != nil {
		t.Fatal(err)
	}

	first, err := repo.ClaimScan(ctx, time.Minute)
	if err != nil {
		t.Fatalf("first ClaimScan() error = %v", err)
	}
	if first.UploadID != upload.ID || first.LeaseToken == "" || first.Attempts != 1 {
		t.Fatalf("first claim = %#v", first)
	}
	if _, err := repo.ClaimScan(ctx, time.Minute); !errors.Is(err, ErrNoScanJobs) {
		t.Fatalf("second ClaimScan() error = %v, want ErrNoScanJobs", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE media_scan_jobs SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimScan(ctx, time.Minute)
	if err != nil {
		t.Fatalf("reclaimed ClaimScan() error = %v", err)
	}
	if second.ID != first.ID || second.LeaseToken == first.LeaseToken || second.Attempts != 2 {
		t.Fatalf("reclaimed claim = %#v, first = %#v", second, first)
	}
	if err := repo.CompleteScan(ctx, first.ID, first.LeaseToken, ScanResult{Clean: true, AvailableObjectKey: "available/stale.png"}); !errors.Is(err, ErrScanLeaseLost) {
		t.Fatalf("stale CompleteScan() error = %v, want ErrScanLeaseLost", err)
	}
	if err := repo.CompleteScan(ctx, second.ID, second.LeaseToken, ScanResult{Clean: true, AvailableObjectKey: "available/current.png"}); err != nil {
		t.Fatalf("current CompleteScan() error = %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM media_uploads WHERE id=$1`, upload.ID).Scan(&status); err != nil || status != string(UploadAvailable) {
		t.Fatalf("clean upload status = %q, err %v; want available", status, err)
	}
}

func TestMediaRepositoryConcurrentClaimsDoNotReturnSameJob(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	var uploadIDs []string
	for i := range 2 {
		input := validUpload(owner)
		input.ObjectKey = fmt.Sprintf("posts/%s/%d.png", owner, i)
		upload, err := repo.CreateUpload(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.QueueScan(ctx, upload.ID, "image/png", input.DeclaredSize); err != nil {
			t.Fatal(err)
		}
		uploadIDs = append(uploadIDs, upload.ID)
	}

	start := make(chan struct{})
	type claimResult struct {
		job ScanJob
		err error
	}
	results := make(chan claimResult, 2)
	for range 2 {
		go func() {
			<-start
			job, err := repo.ClaimScan(ctx, time.Minute)
			results <- claimResult{job: job, err: err}
		}()
	}
	close(start)
	firstResult, secondResult := <-results, <-results
	if firstResult.err != nil || secondResult.err != nil {
		t.Fatalf("concurrent ClaimScan() errors = %v, %v", firstResult.err, secondResult.err)
	}
	first, second := firstResult.job, secondResult.job
	if first.ID == second.ID || first.UploadID == second.UploadID {
		t.Fatalf("concurrent claims returned the same job: %#v and %#v", first, second)
	}
}

func TestMediaRepositoryRetriesScannerFailuresAndRejectsAfterAttemptLimit(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	upload, err := repo.CreateUpload(ctx, validUpload(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.QueueScan(ctx, upload.ID, "image/png", upload.DeclaredSize); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= maxScanAttempts; attempt++ {
		claim, err := repo.ClaimScan(ctx, time.Minute)
		if err != nil {
			t.Fatalf("ClaimScan() attempt %d: %v", attempt, err)
		}
		if claim.Attempts != attempt {
			t.Fatalf("claim attempts = %d, want %d", claim.Attempts, attempt)
		}
		if err := repo.FailScan(ctx, claim.ID, claim.LeaseToken, "scanner_unavailable", 10*time.Second); err != nil {
			t.Fatalf("FailScan() attempt %d: %v", attempt, err)
		}
		if attempt < maxScanAttempts {
			if _, err := repo.ClaimScan(ctx, time.Minute); !errors.Is(err, ErrNoScanJobs) {
				t.Fatalf("ClaimScan() ignored retry delay, error = %v", err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE media_scan_jobs SET available_at=now()-interval '1 second' WHERE id=$1`, claim.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := repo.ClaimScan(ctx, time.Minute); !errors.Is(err, ErrNoScanJobs) {
		t.Fatalf("ClaimScan() returned dead job, error = %v", err)
	}
	var jobStatus, uploadStatus string
	if err := db.QueryRowContext(ctx, `SELECT j.status,u.status FROM media_scan_jobs j JOIN media_uploads u ON u.id=j.upload_id WHERE u.id=$1`, upload.ID).Scan(&jobStatus, &uploadStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "dead" || uploadStatus != string(UploadScanFailed) {
		t.Fatalf("terminal states = job:%q upload:%q", jobStatus, uploadStatus)
	}
}

func TestMediaRepositoryValidatesInputsAndPersistsMalwareRejection(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	repo := NewRepository(db)
	bad := validUpload(owner)
	bad.DeclaredSize = 0
	if _, err := repo.CreateUpload(ctx, bad); !errors.Is(err, ErrInvalidMediaUpload) {
		t.Fatalf("CreateUpload() invalid size error = %v", err)
	}
	bad = validUpload(owner)
	bad.Purpose = PurposeSermonAttachment
	if _, err := repo.CreateUpload(ctx, bad); !errors.Is(err, ErrInvalidMediaUpload) {
		t.Fatalf("CreateUpload() mismatched purpose/key error = %v", err)
	}
	if _, err := repo.CreateUpload(ctx, CreateUploadInput{OwnerID: "00000000-0000-0000-0000-000000000000", Purpose: PurposePostAttachment, ObjectKey: "posts/x.png", DeclaredContentType: "image/png", DeclaredSize: 10}); err == nil {
		t.Fatal("CreateUpload() accepted a nonexistent owner")
	}

	upload, err := repo.CreateUpload(ctx, validUpload(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.QueueScan(ctx, upload.ID, "image/png", upload.DeclaredSize); err != nil {
		t.Fatal(err)
	}
	claim, err := repo.ClaimScan(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteScan(ctx, claim.ID, claim.LeaseToken, ScanResult{}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM media_uploads WHERE id=$1`, upload.ID).Scan(&status); err != nil || status != string(UploadRejected) {
		t.Fatalf("infected upload status = %q, err %v; want rejected", status, err)
	}
}

func mediaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func mediaTestAccount(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text || '@media.test.invalid','Media','Test',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func validUpload(owner string) CreateUploadInput {
	return CreateUploadInput{
		OwnerID: owner, Purpose: PurposePostAttachment, ObjectKey: fmt.Sprintf("posts/%s/image.png", owner),
		DeclaredContentType: "image/png", DeclaredSize: 128, ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}
}

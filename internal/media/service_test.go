package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUploadStorage struct {
	request SignedRequest
	info    ObjectInfo
	err     error
	keys    []string
	now     func() time.Time
}

func (s *fakeUploadStorage) PresignUpload(_ context.Context, key, contentType string, size int64, ttl time.Duration) (SignedRequest, error) {
	s.keys = append(s.keys, key)
	request := s.request
	request.Method = "PUT"
	request.Headers = map[string]string{"Content-Type": contentType, "Content-Length": "128"}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	request.ExpiresAt = now().UTC().Add(ttl)
	return request, nil
}

func (s *fakeUploadStorage) HeadObject(context.Context, string) (ObjectInfo, error) {
	return s.info, s.err
}

func TestMediaServiceCreatesShortLivedOwnerScopedUpload(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	now := time.Now().UTC().Truncate(time.Second)
	storage := &fakeUploadStorage{request: SignedRequest{URL: "https://r2.example.invalid/upload"}, now: func() time.Time { return now }}
	service := NewService(NewRepository(db), storage, func() time.Time { return now })
	result, err := service.Presign(ctx, owner, PresignInput{Purpose: PurposePostAttachment, ContentType: "image/png", Size: 128})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	if result.UploadID == "" || result.Method != "PUT" || result.URL != storage.request.URL || result.ExpiresAt.Sub(now) != uploadURLTTL {
		t.Fatalf("presign result = %#v", result)
	}
	if len(storage.keys) != 1 || !validObjectKey(storage.keys[0]) || len(storage.keys[0]) < len("posts/"+owner+"/") || storage.keys[0][:len("posts/"+owner+"/")] != "posts/"+owner+"/" {
		t.Fatalf("object key is not safe and owner scoped: %#v", storage.keys)
	}
	if result.Headers["Content-Type"] != "image/png" || result.Headers["Content-Length"] != "128" {
		t.Fatalf("signed headers = %#v", result.Headers)
	}

	other := mediaTestAccount(t, db)
	if _, err := service.Status(ctx, other, result.UploadID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("Status() for other owner = %v, want not found", err)
	}
	status, err := service.Status(ctx, owner, result.UploadID)
	if err != nil || status.Status != UploadPending {
		t.Fatalf("owner status = %#v, %v; want pending", status, err)
	}
}

func TestMediaServiceValidatesMediaPolicyBeforeSigning(t *testing.T) {
	db := mediaTestDB(t)
	owner := mediaTestAccount(t, db)
	storage := &fakeUploadStorage{}
	service := NewService(NewRepository(db), storage, time.Now)
	for _, input := range []PresignInput{
		{Purpose: PurposePostAttachment, ContentType: "image/svg+xml", Size: 100},
		{Purpose: PurposePostAttachment, ContentType: "application/pdf", Size: 100},
		{Purpose: PurposePostAttachment, ContentType: "image/png", Size: maxImageSize + 1},
		{Purpose: PurposeSermonAttachment, ContentType: "video/mp4", Size: 0},
		{Purpose: "unknown", ContentType: "image/png", Size: 100},
	} {
		if _, err := service.Presign(context.Background(), owner, input); !errors.Is(err, ErrInvalidMediaUpload) {
			t.Errorf("Presign(%#v) error = %v, want ErrInvalidMediaUpload", input, err)
		}
	}
	if len(storage.keys) != 0 {
		t.Fatalf("invalid requests reached storage: %#v", storage.keys)
	}
}

func TestMediaServiceCompletionChecksR2MetadataAndQueuesOnce(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	storage := &fakeUploadStorage{request: SignedRequest{URL: "https://r2.example.invalid/upload"}, info: ObjectInfo{ContentLength: 128, ContentType: "image/png"}}
	service := NewService(NewRepository(db), storage, time.Now)
	reserved, err := service.Presign(ctx, owner, PresignInput{Purpose: PurposePostAttachment, ContentType: "image/png", Size: 128})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Complete(ctx, owner, reserved.UploadID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.Status != UploadScanQueued {
		t.Fatalf("completed upload status = %q, want scan_queued", completed.Status)
	}
	if _, err := service.Complete(ctx, owner, reserved.UploadID); err != nil {
		t.Fatalf("idempotent Complete() error = %v", err)
	}
	var jobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs WHERE upload_id=$1`, reserved.UploadID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("completion jobs = %d, err %v; want one", jobs, err)
	}
}

func TestMediaServiceCompletionDoesNotQueueMissingOrMismatchedObjects(t *testing.T) {
	for _, tc := range []struct {
		name string
		info ObjectInfo
		err  error
		want error
	}{
		{name: "missing", err: ErrObjectNotFound, want: ErrUploadNotComplete},
		{name: "wrong size", info: ObjectInfo{ContentLength: 127, ContentType: "image/png"}, want: ErrUploadMismatch},
		{name: "wrong type", info: ObjectInfo{ContentLength: 128, ContentType: "image/jpeg"}, want: ErrUploadMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := mediaTestDB(t)
			ctx := context.Background()
			owner := mediaTestAccount(t, db)
			storage := &fakeUploadStorage{request: SignedRequest{URL: "https://r2.example.invalid/upload"}, info: tc.info, err: tc.err}
			service := NewService(NewRepository(db), storage, time.Now)
			reserved, err := service.Presign(ctx, owner, PresignInput{Purpose: PurposePostAttachment, ContentType: "image/png", Size: 128})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Complete(ctx, owner, reserved.UploadID); !errors.Is(err, tc.want) {
				t.Fatalf("Complete() error = %v, want %v", err, tc.want)
			}
			var jobs int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs WHERE upload_id=$1`, reserved.UploadID).Scan(&jobs); err != nil || jobs != 0 {
				t.Fatalf("mismatched object queued %d scan jobs, err %v", jobs, err)
			}
		})
	}
}

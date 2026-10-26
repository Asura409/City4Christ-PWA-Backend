package media

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestProcessorSniffsScansAndPromotesCleanUploadToImmutableKey(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	content := []byte("\x89PNG\r\n\x1a\nclean image payload")
	upload := prepareMediaScan(t, db, owner, content, "image/png")
	store := newMemoryScanStorage(upload.ObjectKey, content, ObjectInfo{ContentLength: int64(len(content)), ContentType: "image/png"})
	scanner := &recordingScanner{}
	processor := NewProcessor(NewRepository(db), store, scanner, ProcessorConfig{LeaseDuration: time.Minute})

	worked, err := processor.ProcessNext(ctx)
	if err != nil || !worked {
		t.Fatalf("ProcessNext() = (%v, %v), want (true, nil)", worked, err)
	}
	got, err := NewRepository(db).ByOwner(ctx, upload.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != UploadAvailable || !got.AvailableObjectKey.Valid || got.AvailableObjectKey.String == upload.ObjectKey {
		t.Fatalf("completed upload = status %q available key %#v; want a distinct immutable key from source %q", got.Status, got.AvailableObjectKey, upload.ObjectKey)
	}
	if got.ActualContentType.String != "image/png" || !scanner.called {
		t.Fatalf("actual type = %q, scanner called = %v", got.ActualContentType.String, scanner.called)
	}
	if promoted := store.object(got.AvailableObjectKey.String); !bytes.Equal(promoted, content) {
		t.Fatalf("promoted bytes = %q, want %q", promoted, content)
	}
	if store.object(upload.ObjectKey) != nil {
		t.Fatal("temporary source object was not deleted after promotion")
	}
}

func TestProcessorRejectsMIMEMismatchAndMalwareWithoutPromotion(t *testing.T) {
	for _, test := range []struct {
		name      string
		content   []byte
		declared  string
		scanError error
	}{
		{name: "MIME mismatch", content: []byte("plain text"), declared: "image/png"},
		{name: "malware", content: []byte("\x89PNG\r\n\x1a\nmalware"), declared: "image/png", scanError: ErrMalwareDetected},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := mediaTestDB(t)
			ctx := context.Background()
			owner := mediaTestAccount(t, db)
			upload := prepareMediaScan(t, db, owner, test.content, test.declared)
			store := newMemoryScanStorage(upload.ObjectKey, test.content, ObjectInfo{ContentLength: int64(len(test.content)), ContentType: test.declared})
			scanner := &recordingScanner{err: test.scanError}
			processor := NewProcessor(NewRepository(db), store, scanner, ProcessorConfig{LeaseDuration: time.Minute})
			if worked, err := processor.ProcessNext(ctx); err != nil || !worked {
				t.Fatalf("ProcessNext() = (%v, %v), want (true, nil)", worked, err)
			}
			got, err := NewRepository(db).ByOwner(ctx, upload.ID, owner)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != UploadRejected {
				t.Fatalf("upload status = %q, want rejected", got.Status)
			}
			if store.object(upload.ObjectKey) != nil || len(store.keysWithPrefix("available/")) != 0 {
				t.Fatal("rejected bytes remained in storage or were promoted")
			}
		})
	}
}

func TestProcessorRetriesScannerUnavailableWithoutPublishingOrDeletingSource(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	content := []byte("\x89PNG\r\n\x1a\nclean image payload")
	upload := prepareMediaScan(t, db, owner, content, "image/png")
	store := newMemoryScanStorage(upload.ObjectKey, content, ObjectInfo{ContentLength: int64(len(content)), ContentType: "image/png"})
	processor := NewProcessor(NewRepository(db), store, &recordingScanner{err: ErrScannerUnavailable}, ProcessorConfig{LeaseDuration: time.Minute, RetryDelay: time.Second})

	if worked, err := processor.ProcessNext(ctx); err != nil || !worked {
		t.Fatalf("ProcessNext() = (%v, %v), want (true, nil)", worked, err)
	}
	got, err := NewRepository(db).ByOwner(ctx, upload.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != UploadScanQueued || store.object(upload.ObjectKey) == nil || len(store.keysWithPrefix("available/")) != 0 {
		t.Fatalf("after scanner failure status=%q source-present=%v published=%v", got.Status, store.object(upload.ObjectKey) != nil, store.keysWithPrefix("available/"))
	}
}

func TestProcessorRemovesPromotionWhenLeaseWasLostBeforeCommit(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	content := []byte("\x89PNG\r\n\x1a\nclean image payload")
	upload := prepareMediaScan(t, db, owner, content, "image/png")
	store := newMemoryScanStorage(upload.ObjectKey, content, ObjectInfo{ContentLength: int64(len(content)), ContentType: "image/png"})
	repo := NewRepository(db)
	processor := NewProcessor(lostCompletionRepository{ScanRepository: repo}, store, &recordingScanner{}, ProcessorConfig{LeaseDuration: time.Minute})

	if worked, err := processor.ProcessNext(ctx); !worked || !errors.Is(err, ErrScanLeaseLost) {
		t.Fatalf("ProcessNext() = (%v, %v), want (true, ErrScanLeaseLost)", worked, err)
	}
	if len(store.keysWithPrefix("available/")) != 0 || store.object(upload.ObjectKey) == nil {
		t.Fatal("stale worker left a published object or removed the original upload")
	}
	got, err := repo.ByOwner(ctx, upload.ID, owner)
	if err != nil || got.Status != UploadScanning || got.AvailableObjectKey.Valid {
		t.Fatalf("upload after stale worker = %#v, error %v", got, err)
	}
}

type lostCompletionRepository struct{ ScanRepository }

func (lostCompletionRepository) CompleteScan(context.Context, string, string, ScanResult) error {
	return ErrScanLeaseLost
}

func prepareMediaScan(t *testing.T, db *sql.DB, owner string, content []byte, contentType string) Upload {
	t.Helper()
	input := validUpload(owner)
	input.DeclaredSize = int64(len(content))
	input.DeclaredContentType = contentType
	input.ExpiresAt = time.Now().UTC().Add(time.Minute)
	upload, err := NewRepository(db).CreateUpload(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewRepository(db).QueueScan(context.Background(), upload.ID, contentType, int64(len(content))); err != nil {
		t.Fatal(err)
	}
	return upload
}

type recordingScanner struct {
	called bool
	err    error
}

func (s *recordingScanner) Scan(context.Context, string) error { s.called = true; return s.err }

type memoryScanStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
	info    map[string]ObjectInfo
}

func newMemoryScanStorage(key string, content []byte, info ObjectInfo) *memoryScanStorage {
	return &memoryScanStorage{objects: map[string][]byte{key: append([]byte(nil), content...)}, info: map[string]ObjectInfo{key: info}}
}

func (s *memoryScanStorage) GetObject(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.objects[key]
	if !ok {
		return nil, ObjectInfo{}, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(content)), s.info[key], nil
}

func (s *memoryScanStorage) PutObject(_ context.Context, key string, body io.Reader, size int64, contentType string) error {
	content, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(content)) != size {
		return errors.New("size mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = content
	s.info[key] = ObjectInfo{ContentLength: size, ContentType: contentType}
	return nil
}

func (s *memoryScanStorage) DeleteObject(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	delete(s.info, key)
	return nil
}

func (s *memoryScanStorage) object(key string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.objects[key]...)
}

func (s *memoryScanStorage) keysWithPrefix(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for key := range s.objects {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys
}

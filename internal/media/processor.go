package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
)

const defaultScanLease = 5 * time.Minute

type ScanRepository interface {
	ClaimScan(context.Context, time.Duration) (ScanJob, error)
	CompleteScan(context.Context, string, string, ScanResult) error
	FailScan(context.Context, string, string, string, time.Duration) error
}

type Scanner interface {
	Scan(context.Context, string) error
}

type WorkerStorage interface {
	GetObject(context.Context, string) (io.ReadCloser, ObjectInfo, error)
	PutObject(context.Context, string, io.Reader, int64, string) error
	DeleteObject(context.Context, string) error
}

type ProcessorConfig struct {
	LeaseDuration time.Duration
	RetryDelay    time.Duration
	TempDir       string
}

type Processor struct {
	repo    ScanRepository
	store   WorkerStorage
	scanner Scanner
	config  ProcessorConfig
}

func NewProcessor(repo ScanRepository, store WorkerStorage, scanner Scanner, config ProcessorConfig) *Processor {
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultScanLease
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 15 * time.Second
	}
	return &Processor{repo: repo, store: store, scanner: scanner, config: config}
}

// ProcessNext claims and processes no more than one durable scan job.
// It reports worked=false only when there is currently no eligible job.
func (p *Processor) ProcessNext(ctx context.Context) (worked bool, err error) {
	if p == nil || p.repo == nil || p.store == nil || p.scanner == nil {
		return false, ErrMediaUnavailable
	}
	job, err := p.repo.ClaimScan(ctx, p.config.LeaseDuration)
	if errors.Is(err, ErrNoScanJobs) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := p.process(ctx, job); err != nil {
		return true, err
	}
	return true, nil
}

func (p *Processor) process(ctx context.Context, job ScanJob) error {
	expectedType, _, maxSize, _, allowed := mediaPolicy(job.Purpose, job.ContentType)
	if !allowed || job.Size <= 0 || job.Size > maxSize {
		return p.reject(ctx, job)
	}

	body, info, err := p.store.GetObject(ctx, job.ObjectKey)
	if errors.Is(err, ErrObjectNotFound) {
		return p.reject(ctx, job)
	}
	if err != nil {
		return p.retry(ctx, job, "storage_unavailable")
	}
	defer body.Close()
	if info.ContentLength != job.Size || normalizeMediaType(info.ContentType) != normalizeMediaType(expectedType) {
		return p.reject(ctx, job)
	}

	file, err := os.CreateTemp(p.config.TempDir, "c4c-media-scan-*")
	if err != nil {
		return p.retry(ctx, job, "scanner_unavailable")
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return p.retry(ctx, job, "scanner_unavailable")
	}
	maxRead := job.Size + 1
	if maxRead < 0 {
		maxRead = job.Size
	}
	read, copyErr := io.Copy(file, io.LimitReader(body, maxRead))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return p.retry(ctx, job, "storage_unavailable")
	}
	if read != job.Size {
		return p.reject(ctx, job)
	}

	detected, err := mimetype.DetectFile(path)
	if err != nil {
		return p.retry(ctx, job, "scanner_unavailable")
	}
	if normalizeMediaType(detected.String()) != normalizeMediaType(expectedType) {
		return p.reject(ctx, job)
	}

	scanErr := p.scanner.Scan(ctx, path)
	switch {
	case errors.Is(scanErr, ErrMalwareDetected):
		return p.reject(ctx, job)
	case scanErr != nil:
		return p.retry(ctx, job, "scanner_unavailable")
	}

	_, extension, _, _, _ := mediaPolicy(job.Purpose, expectedType)
	availableKey := "available/" + job.UploadID + "/" + uuid.NewString() + "." + extension
	cleanFile, err := os.Open(path)
	if err != nil {
		return p.retry(ctx, job, "storage_unavailable")
	}
	err = p.store.PutObject(ctx, availableKey, cleanFile, job.Size, expectedType)
	closeErr = cleanFile.Close()
	if err != nil || closeErr != nil {
		_ = p.store.DeleteObject(ctx, availableKey)
		return p.retry(ctx, job, "storage_unavailable")
	}
	if err := p.repo.CompleteScan(ctx, job.ID, job.LeaseToken, ScanResult{Clean: true, AvailableObjectKey: availableKey}); err != nil {
		_ = p.store.DeleteObject(ctx, availableKey)
		return err
	}
	return p.store.DeleteObject(ctx, job.ObjectKey)
}

func (p *Processor) reject(ctx context.Context, job ScanJob) error {
	if err := p.repo.CompleteScan(ctx, job.ID, job.LeaseToken, ScanResult{}); err != nil {
		return err
	}
	if err := p.store.DeleteObject(ctx, job.ObjectKey); err != nil && !errors.Is(err, ErrObjectNotFound) {
		return fmt.Errorf("remove rejected media object: %w", err)
	}
	return nil
}

func (p *Processor) retry(ctx context.Context, job ScanJob, code string) error {
	return p.repo.FailScan(ctx, job.ID, job.LeaseToken, code, p.config.RetryDelay)
}

func normalizeMediaType(raw string) string {
	parsed, _, err := mime.ParseMediaType(raw)
	if err == nil {
		raw = parsed
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "image/jpg":
		return "image/jpeg"
	case "audio/x-wav", "audio/wave":
		return "audio/wav"
	case "audio/mp3":
		return "audio/mpeg"
	default:
		return raw
	}
}

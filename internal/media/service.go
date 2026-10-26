package media

import (
	"context"
	"errors"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	uploadURLTTL               = 5 * time.Minute
	uploadReservationTTL       = 15 * time.Minute
	maxImageSize         int64 = 20 << 20
	maxAudioSize         int64 = 50 << 20
	maxVideoSize         int64 = 100 << 20
)

var (
	ErrUploadNotComplete = errors.New("media upload object is not available yet")
	ErrMediaUnavailable  = errors.New("media uploads are not configured")
)

type PresignInput struct {
	Purpose     UploadPurpose `json:"purpose"`
	ContentType string        `json:"contentType"`
	Size        int64         `json:"size"`
}

type PresignResult struct {
	UploadID  string            `json:"uploadId"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

type Service struct {
	repo  *Repository
	store UploadStorage
	now   func() time.Time
}

func NewService(repo *Repository, store UploadStorage, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, store: store, now: now}
}

func (s *Service) Presign(ctx context.Context, ownerID string, input PresignInput) (PresignResult, error) {
	if s == nil || s.repo == nil || s.store == nil {
		return PresignResult{}, ErrMediaUnavailable
	}
	contentType, extension, maxSize, folder, ok := mediaPolicy(input.Purpose, input.ContentType)
	if !ok || input.Size < 1 || input.Size > maxSize || uuid.Validate(ownerID) != nil {
		return PresignResult{}, ErrInvalidMediaUpload
	}
	key := folder + "/" + ownerID + "/" + uuid.NewString() + "." + extension
	now := s.now().UTC()
	signed, err := s.store.PresignUpload(ctx, key, contentType, input.Size, uploadURLTTL)
	if err != nil {
		return PresignResult{}, err
	}
	upload, err := s.repo.CreateUpload(ctx, CreateUploadInput{
		OwnerID: ownerID, Purpose: input.Purpose, ObjectKey: key,
		DeclaredContentType: contentType, DeclaredSize: input.Size,
		ExpiresAt: now.Add(uploadReservationTTL),
	})
	if err != nil {
		return PresignResult{}, err
	}
	headers := make(map[string]string, len(signed.Headers))
	for name, value := range signed.Headers {
		headers[name] = value
	}
	return PresignResult{UploadID: upload.ID, URL: signed.URL, Method: signed.Method, Headers: headers, ExpiresAt: signed.ExpiresAt}, nil
}

func (s *Service) Complete(ctx context.Context, ownerID, uploadID string) (Upload, error) {
	if s == nil || s.repo == nil {
		return Upload{}, ErrMediaUnavailable
	}
	upload, err := s.repo.ByOwner(ctx, uploadID, ownerID)
	if err != nil {
		return Upload{}, err
	}
	if upload.Status != UploadPending {
		return upload, nil
	}
	expired, err := s.repo.ExpirePending(ctx, uploadID, ownerID)
	if err != nil {
		return Upload{}, err
	}
	if expired {
		return Upload{}, ErrUploadExpired
	}
	if s.store == nil {
		return Upload{}, ErrMediaUnavailable
	}
	info, err := s.store.HeadObject(ctx, upload.ObjectKey)
	if errors.Is(err, ErrObjectNotFound) {
		return Upload{}, ErrUploadNotComplete
	}
	if err != nil {
		return Upload{}, err
	}
	if _, err := s.repo.QueueScan(ctx, upload.ID, info.ContentType, info.ContentLength); err != nil {
		return Upload{}, err
	}
	return s.repo.ByOwner(ctx, upload.ID, ownerID)
}

func (s *Service) Status(ctx context.Context, ownerID, uploadID string) (Upload, error) {
	if s == nil || s.repo == nil {
		return Upload{}, ErrMediaUnavailable
	}
	if _, err := s.repo.ExpirePending(ctx, uploadID, ownerID); err != nil {
		return Upload{}, err
	}
	return s.repo.ByOwner(ctx, uploadID, ownerID)
}

func mediaPolicy(purpose UploadPurpose, rawContentType string) (contentType, extension string, maxSize int64, folder string, ok bool) {
	parsed, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		return "", "", 0, "", false
	}
	contentType = strings.ToLower(parsed)
	switch contentType {
	case "image/jpeg":
		extension, maxSize = "jpg", maxImageSize
	case "image/png":
		extension, maxSize = "png", maxImageSize
	case "image/webp":
		extension, maxSize = "webp", maxImageSize
	case "image/gif":
		extension, maxSize = "gif", maxImageSize
	case "audio/mpeg":
		extension, maxSize = "mp3", maxAudioSize
	case "audio/mp4":
		extension, maxSize = "m4a", maxAudioSize
	case "audio/wav", "audio/x-wav":
		contentType, extension, maxSize = "audio/wav", "wav", maxAudioSize
	case "audio/ogg":
		extension, maxSize = "ogg", maxAudioSize
	case "video/mp4":
		extension, maxSize = "mp4", maxVideoSize
	case "video/webm":
		extension, maxSize = "webm", maxVideoSize
	default:
		return "", "", 0, "", false
	}
	switch purpose {
	case PurposePostAttachment:
		folder = "posts"
	case PurposeSermonAttachment:
		folder = "sermons"
	default:
		return "", "", 0, "", false
	}
	return contentType, extension, maxSize, folder, true
}

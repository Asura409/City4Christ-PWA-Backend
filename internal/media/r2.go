// Package media contains provider adapters for private church media storage.
package media

import (
	"context"
	"errors"
	"io"
	"mime"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	maxSignedURLTTL   = 5 * time.Minute
	maxDownloadURLTTL = time.Hour
)

var (
	ErrInvalidR2Config   = errors.New("invalid R2 configuration")
	ErrInvalidMediaURL   = errors.New("invalid media URL request")
	ErrSignMediaURL      = errors.New("could not sign media URL")
	ErrObjectNotFound    = errors.New("media object not found")
	ErrObjectUnavailable = errors.New("media object storage unavailable")

	accountIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
	bucketPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
	objectKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
)

type R2Config struct {
	AccountID       string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

type SignedRequest struct {
	URL       string
	Method    string
	ExpiresAt time.Time
	Headers   map[string]string
}

type ObjectInfo struct {
	ContentLength int64
	ContentType   string
}

type R2Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	now       func() time.Time
}

// R2Signer remains as a source-compatible name for the original signing adapter.
type R2Signer = R2Storage

func NewR2Storage(config R2Config, now func() time.Time) (*R2Storage, error) {
	return newR2Storage(config, now, "")
}

func NewR2Signer(config R2Config, now func() time.Time) (*R2Signer, error) {
	return NewR2Storage(config, now)
}

func newR2Storage(config R2Config, now func() time.Time, endpoint string) (*R2Storage, error) {
	if !accountIDPattern.MatchString(config.AccountID) ||
		!bucketPattern.MatchString(config.Bucket) ||
		config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return nil, ErrInvalidR2Config
	}
	if now == nil {
		now = time.Now
	}
	if endpoint == "" {
		endpoint = "https://" + config.AccountID + ".r2.cloudflarestorage.com"
	}

	client := s3.NewFromConfig(aws.Config{
		Region:      "auto",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})

	return &R2Storage{client: client, presigner: s3.NewPresignClient(client), bucket: config.Bucket, now: now}, nil
}

type UploadStorage interface {
	PresignUpload(context.Context, string, string, int64, time.Duration) (SignedRequest, error)
	HeadObject(context.Context, string) (ObjectInfo, error)
}

func (s *R2Storage) PutObject(ctx context.Context, objectKey string, body io.Reader, contentLength int64, contentType string) error {
	if s == nil || s.client == nil || body == nil || !validObjectKey(objectKey) || contentLength < 1 || !validUploadContentType(contentType) {
		return ErrInvalidMediaURL
	}
	contentType, _, _ = mime.ParseMediaType(contentType)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), Body: body,
		ContentLength: aws.Int64(contentLength), ContentType: aws.String(contentType),
	})
	if err != nil {
		return objectStorageError(err)
	}
	return nil
}

func (s *R2Storage) DeleteObject(ctx context.Context, objectKey string) error {
	if s == nil || s.client == nil || !validObjectKey(objectKey) {
		return ErrInvalidMediaURL
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return objectStorageError(err)
	}
	return nil
}

func (s *R2Storage) PresignUpload(ctx context.Context, objectKey, contentType string, contentLength int64, ttl time.Duration) (SignedRequest, error) {
	if s == nil || s.presigner == nil || !validObjectKey(objectKey) || !validTTL(ttl) || contentLength < 1 || !validUploadContentType(contentType) {
		return SignedRequest{}, ErrInvalidMediaURL
	}
	contentType, _, _ = mime.ParseMediaType(contentType)
	request, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
	}, func(options *s3.PresignOptions) {
		options.Expires = ttl
	})
	if err != nil || request == nil {
		return SignedRequest{}, ErrSignMediaURL
	}
	return SignedRequest{
		URL:       request.URL,
		Method:    request.Method,
		ExpiresAt: s.now().UTC().Add(ttl),
		Headers: map[string]string{
			"Content-Type":   contentType,
			"Content-Length": strconv.FormatInt(contentLength, 10),
		},
	}, nil
}

func (s *R2Storage) PresignDownload(ctx context.Context, objectKey string, ttl time.Duration) (SignedRequest, error) {
	if s == nil || s.presigner == nil || !validObjectKey(objectKey) || ttl <= 0 || ttl > maxDownloadURLTTL {
		return SignedRequest{}, ErrInvalidMediaURL
	}
	request, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}, func(options *s3.PresignOptions) {
		options.Expires = ttl
	})
	if err != nil || request == nil {
		return SignedRequest{}, ErrSignMediaURL
	}
	return SignedRequest{URL: request.URL, Method: request.Method, ExpiresAt: s.now().UTC().Add(ttl)}, nil
}

func (s *R2Storage) HeadObject(ctx context.Context, objectKey string) (ObjectInfo, error) {
	if s == nil || s.client == nil || !validObjectKey(objectKey) {
		return ObjectInfo{}, ErrInvalidMediaURL
	}
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return ObjectInfo{}, objectStorageError(err)
	}
	if result.ContentLength == nil {
		return ObjectInfo{}, ErrObjectUnavailable
	}
	return ObjectInfo{ContentLength: *result.ContentLength, ContentType: aws.ToString(result.ContentType)}, nil
}

func (s *R2Storage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, ObjectInfo, error) {
	if s == nil || s.client == nil || !validObjectKey(objectKey) {
		return nil, ObjectInfo{}, ErrInvalidMediaURL
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return nil, ObjectInfo{}, objectStorageError(err)
	}
	if result.ContentLength == nil || result.Body == nil {
		if result.Body != nil {
			_ = result.Body.Close()
		}
		return nil, ObjectInfo{}, ErrObjectUnavailable
	}
	return result.Body, ObjectInfo{ContentLength: *result.ContentLength, ContentType: aws.ToString(result.ContentType)}, nil
}

func objectStorageError(err error) error {
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == 404 {
		return ErrObjectNotFound
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchObject":
			return ErrObjectNotFound
		}
	}
	return ErrObjectUnavailable
}

func validTTL(ttl time.Duration) bool { return ttl > 0 && ttl <= maxSignedURLTTL }

func validObjectKey(key string) bool {
	if len(key) > 1024 || !objectKeyPattern.MatchString(key) || strings.HasPrefix(key, "/") || strings.Contains(key, "//") {
		return false
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validUploadContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/")
}

// Package media contains provider adapters for private church media storage.
package media

import (
	"context"
	"errors"
	"mime"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const maxSignedURLTTL = 5 * time.Minute

var (
	ErrInvalidR2Config = errors.New("invalid R2 configuration")
	ErrInvalidMediaURL = errors.New("invalid media URL request")
	ErrSignMediaURL    = errors.New("could not sign media URL")

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

type R2Signer struct {
	client *s3.PresignClient
	bucket string
	now    func() time.Time
}

func NewR2Signer(config R2Config, now func() time.Time) (*R2Signer, error) {
	if !accountIDPattern.MatchString(config.AccountID) ||
		!bucketPattern.MatchString(config.Bucket) ||
		config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return nil, ErrInvalidR2Config
	}
	if now == nil {
		now = time.Now
	}

	client := s3.NewFromConfig(aws.Config{
		Region:      "auto",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://" + config.AccountID + ".r2.cloudflarestorage.com")
	})

	return &R2Signer{client: s3.NewPresignClient(client), bucket: config.Bucket, now: now}, nil
}

func (s *R2Signer) PresignUpload(ctx context.Context, objectKey, contentType string, contentLength int64, ttl time.Duration) (SignedRequest, error) {
	if s == nil || s.client == nil || !validObjectKey(objectKey) || !validTTL(ttl) || contentLength < 1 || !validUploadContentType(contentType) {
		return SignedRequest{}, ErrInvalidMediaURL
	}
	contentType, _, _ = mime.ParseMediaType(contentType)
	request, err := s.client.PresignPutObject(ctx, &s3.PutObjectInput{
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

func (s *R2Signer) PresignDownload(ctx context.Context, objectKey string, ttl time.Duration) (SignedRequest, error) {
	if s == nil || s.client == nil || !validObjectKey(objectKey) || !validTTL(ttl) {
		return SignedRequest{}, ErrInvalidMediaURL
	}
	request, err := s.client.PresignGetObject(ctx, &s3.GetObjectInput{
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

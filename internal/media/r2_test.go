package media

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewR2SignerRequiresCompleteCredentialsAndBucket(t *testing.T) {
	valid := R2Config{
		AccountID:       "0123456789abcdef0123456789abcdef",
		Bucket:          "c4c-media",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}
	tests := []struct {
		name   string
		change func(*R2Config)
	}{
		{name: "account id", change: func(c *R2Config) { c.AccountID = "" }},
		{name: "bucket", change: func(c *R2Config) { c.Bucket = "" }},
		{name: "access key", change: func(c *R2Config) { c.AccessKeyID = "" }},
		{name: "secret key", change: func(c *R2Config) { c.SecretAccessKey = "" }},
		{name: "invalid account id", change: func(c *R2Config) { c.AccountID = "not-an-account" }},
		{name: "invalid bucket", change: func(c *R2Config) { c.Bucket = "Bucket With Spaces" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.change(&config)
			if _, err := NewR2Signer(config, time.Now); err == nil {
				t.Fatal("expected invalid R2 configuration to fail")
			}
		})
	}
}

func TestR2SignerPresignsShortLivedUploadAndDownload(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	signer, err := NewR2Signer(R2Config{
		AccountID:       "0123456789abcdef0123456789abcdef",
		Bucket:          "c4c-media",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewR2Signer() error = %v", err)
	}
	key := "posts/0123456789abcdef0123456789abcdef/image.png"

	put, err := signer.PresignUpload(context.Background(), key, "image/png", 512, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignUpload() error = %v", err)
	}
	assertPresignedRequest(t, put, "PUT", key, now, 5*time.Minute)
	if put.Headers["Content-Type"] != "image/png" {
		t.Fatalf("upload Content-Type = %q, want image/png", put.Headers["Content-Type"])
	}
	if put.Headers["Content-Length"] != "512" {
		t.Fatalf("upload Content-Length = %q, want 512", put.Headers["Content-Length"])
	}

	get, err := signer.PresignDownload(context.Background(), key, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignDownload() error = %v", err)
	}
	assertPresignedRequest(t, get, "GET", key, now, 5*time.Minute)
}

func TestR2SignerRejectsInvalidKeysAndLongExpiry(t *testing.T) {
	signer, err := NewR2Signer(R2Config{
		AccountID:       "0123456789abcdef0123456789abcdef",
		Bucket:          "c4c-media",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}, time.Now)
	if err != nil {
		t.Fatalf("NewR2Signer() error = %v", err)
	}
	for _, key := range []string{"", "/absolute/key", "posts/../private", "posts\\private", "posts/key?x=1"} {
		if _, err := signer.PresignUpload(context.Background(), key, "image/png", 512, time.Minute); err == nil {
			t.Errorf("PresignUpload(%q) succeeded, want invalid key error", key)
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second, 61 * time.Minute} {
		if _, err := signer.PresignDownload(context.Background(), "posts/id/image.png", ttl); err == nil {
			t.Errorf("PresignDownload(%s) succeeded, want invalid expiry error", ttl)
		}
	}
	if _, err := signer.PresignDownload(context.Background(), "posts/id/image.png", DownloadURLTTL); err != nil {
		t.Fatalf("PresignDownload(%s) rejected the attachment URL lifetime: %v", DownloadURLTTL, err)
	}
	if _, err := signer.PresignUpload(context.Background(), "posts/id/image.png", "not a mime type", 512, time.Minute); err == nil {
		t.Fatal("PresignUpload() accepted an invalid content type")
	}
}

func assertPresignedRequest(t *testing.T, request SignedRequest, method, key string, now time.Time, ttl time.Duration) {
	t.Helper()
	if request.Method != method {
		t.Fatalf("method = %q, want %q", request.Method, method)
	}
	u, err := url.Parse(request.URL)
	if err != nil {
		t.Fatalf("signed URL is invalid: %v", err)
	}
	if u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".r2.cloudflarestorage.com") {
		t.Fatalf("signed URL host = %q, want HTTPS R2 host", request.URL)
	}
	if !strings.Contains(u.Path, key) {
		t.Fatalf("signed URL path %q does not contain object key %q", u.Path, key)
	}
	query := u.Query()
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || query.Get("X-Amz-Signature") == "" {
		t.Fatalf("URL is missing SigV4 signature parameters")
	}
	if method == "PUT" && !strings.Contains(";"+query.Get("X-Amz-SignedHeaders")+";", ";content-type;") {
		t.Fatalf("upload URL does not bind the required Content-Type header")
	}
	if method == "PUT" && !strings.Contains(";"+query.Get("X-Amz-SignedHeaders")+";", ";content-length;") {
		t.Fatalf("upload URL does not bind the requested object size")
	}
	if got := query.Get("X-Amz-Expires"); got != "300" {
		t.Fatalf("URL expiry = %q, want 300 seconds", got)
	}
	if request.ExpiresAt.Sub(now) != ttl {
		t.Fatalf("ExpiresAt delta = %s, want %s", request.ExpiresAt.Sub(now), ttl)
	}
}

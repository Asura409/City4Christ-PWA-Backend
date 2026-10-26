package media

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestR2StorageInspectsAndReadsPrivateObjects(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "128")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "8")
			_, _ = io.WriteString(w, "png data")
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store, err := newR2Storage(validR2Config(), time.Now, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	key := "posts/0123456789abcdef0123456789abcdef/media.png"
	info, err := store.HeadObject(context.Background(), key)
	if err != nil {
		t.Fatalf("HeadObject() error = %v", err)
	}
	if info.ContentLength != 128 || info.ContentType != "image/png" {
		t.Fatalf("HeadObject() = %#v", info)
	}
	body, getInfo, err := store.GetObject(context.Background(), key)
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "png data" || getInfo.ContentType != "image/png" || getInfo.ContentLength != 8 {
		t.Fatalf("GetObject() body=%q info=%#v err=%v", data, getInfo, err)
	}
	if len(requests) != 2 || requests[0] != "HEAD /c4c-media/"+key || requests[1] != "GET /c4c-media/"+key {
		t.Fatalf("object requests = %#v", requests)
	}
}

func TestR2StorageMapsMissingAndUnavailableObjects(t *testing.T) {
	status := http.StatusNotFound
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer server.Close()
	store, err := newR2Storage(validR2Config(), time.Now, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	key := "posts/0123456789abcdef0123456789abcdef/media.png"
	if _, err := store.HeadObject(context.Background(), key); err != ErrObjectNotFound {
		t.Fatalf("HeadObject(404) error = %v, want ErrObjectNotFound", err)
	}
	status = http.StatusInternalServerError
	if _, err := store.HeadObject(context.Background(), key); err != ErrObjectUnavailable {
		t.Fatalf("HeadObject(500) error = %v, want ErrObjectUnavailable", err)
	}
	status = http.StatusNotFound
	if _, _, err := store.GetObject(context.Background(), key); err != ErrObjectNotFound {
		t.Fatalf("GetObject(404) error = %v, want ErrObjectNotFound", err)
	}
	for _, unsafe := range []string{"", "../secret", "/absolute", "posts//empty"} {
		if _, err := store.HeadObject(context.Background(), unsafe); err != ErrInvalidMediaURL {
			t.Errorf("HeadObject(%q) error = %v, want invalid key", unsafe, err)
		}
	}
}

func TestR2StoragePromotesAndDeletesPrivateObjects(t *testing.T) {
	var putBody, putType, putPath, deletePath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			putPath = r.URL.Path
			putType = r.Header.Get("Content-Type")
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read PUT body: %v", err)
			}
			putBody = string(data)
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			deletePath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store, err := newR2Storage(validR2Config(), time.Now, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	key := "available/01234567-89ab-cdef-0123-456789abcdef/clean.png"
	if err := store.PutObject(context.Background(), key, strings.NewReader("clean bytes"), int64(len("clean bytes")), "image/png"); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	if err := store.DeleteObject(context.Background(), "posts/0123456789abcdef0123456789abcdef/upload.png"); err != nil {
		t.Fatalf("DeleteObject() error = %v", err)
	}
	if putPath != "/c4c-media/"+key || putBody != "clean bytes" || putType != "image/png" {
		t.Fatalf("PUT path=%q body=%q content-type=%q", putPath, putBody, putType)
	}
	if deletePath != "/c4c-media/posts/0123456789abcdef0123456789abcdef/upload.png" {
		t.Fatalf("DELETE path = %q", deletePath)
	}
}

func validR2Config() R2Config {
	return R2Config{AccountID: "0123456789abcdef0123456789abcdef", Bucket: "c4c-media", AccessKeyID: "test-access", SecretAccessKey: "test-secret"}
}

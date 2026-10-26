package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

type testMediaStorage struct {
	info media.ObjectInfo
	err  error
}

func (s *testMediaStorage) PresignUpload(_ context.Context, _ string, contentType string, size int64, ttl time.Duration) (media.SignedRequest, error) {
	return media.SignedRequest{
		URL: "https://r2.example.invalid/upload", Method: http.MethodPut,
		ExpiresAt: time.Now().UTC().Add(ttl),
		Headers:   map[string]string{"Content-Type": contentType, "Content-Length": "128"},
	}, nil
}

func (s *testMediaStorage) HeadObject(context.Context, string) (media.ObjectInfo, error) {
	return s.info, s.err
}

func (s *testMediaStorage) PresignDownload(_ context.Context, key string, ttl time.Duration) (media.SignedRequest, error) {
	return media.SignedRequest{URL: "https://r2.example.invalid/" + key + "?signature=test", ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

func TestPostAttachmentThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("p", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('post-media@example.invalid','Post','Member',now()) RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	var uploadID string
	if err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES(gen_random_uuid(),$1,'post_attachment','posts/source.png','available/clean/photo.png','image/png',128,'image/png',128,'available',now()+interval '1 hour',now(),now()) RETURNING id::text`, owner).Scan(&uploadID); err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://app.example", MediaStorage: &testMediaStorage{info: media.ObjectInfo{ContentLength: 128, ContentType: "image/png"}}})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		req.Header.Set("Origin", "https://app.example")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	created := request(http.MethodPost, "/api/v1/posts", `{"body":"Photo update","attachmentIds":["`+uploadID+`"]}`)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), "https://r2.example.invalid/available/clean/photo.png") || strings.Contains(created.Body.String(), "objectKey") {
		t.Fatalf("create attachment: %d %s", created.Code, created.Body.String())
	}
	listed := request(http.MethodGet, "/api/v1/posts", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "\"attachments\":[{\"id\":\""+uploadID) {
		t.Fatalf("list attachment: %d %s", listed.Code, listed.Body.String())
	}
}

func TestUploadRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("m", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Media','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	owner, token := account("upload-owner@example.invalid")
	_, otherToken := account("upload-other@example.invalid")
	storage := &testMediaStorage{info: media.ObjectInfo{ContentLength: 128, ContentType: "image/png"}}
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example", MediaStorage: storage})
	request := func(method, path, authToken, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if authToken != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		return response
	}

	if response := request(http.MethodPost, "/api/v1/uploads/presign", "", `{"purpose":"post_attachment","contentType":"image/png","size":128}`, "https://api.example"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous presign: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPost, "/api/v1/uploads/presign", token, `{"purpose":"post_attachment","contentType":"image/png","size":128}`, ""); response.Code != http.StatusForbidden {
		t.Fatalf("presign without Origin: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPost, "/api/v1/uploads/presign", token, `{"purpose":"post_attachment","contentType":"image/svg+xml","size":128}`, "https://api.example"); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported media type: %d %s", response.Code, response.Body.String())
	}
	disabled := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	disabledRequest := httptest.NewRequest(http.MethodPost, "/api/v1/uploads/presign", strings.NewReader(`{"purpose":"post_attachment","contentType":"image/png","size":128}`))
	disabledRequest.Header.Set("Authorization", "Bearer "+token)
	disabledRequest.Header.Set("Content-Type", "application/json")
	disabledRequest.Header.Set("Origin", "https://api.example")
	disabledResponse := httptest.NewRecorder()
	disabled.ServeHTTP(disabledResponse, disabledRequest)
	if disabledResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("presign with unconfigured R2: %d %s", disabledResponse.Code, disabledResponse.Body.String())
	}

	response := request(http.MethodPost, "/api/v1/uploads/presign", token, `{"purpose":"post_attachment","contentType":"image/png","size":128}`, "https://api.example")
	if response.Code != http.StatusCreated {
		t.Fatalf("presign: %d %s", response.Code, response.Body.String())
	}
	var presigned struct {
		Data struct {
			UploadID string `json:"uploadId"`
			URL      string `json:"url"`
			Method   string `json:"method"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &presigned); err != nil {
		t.Fatal(err)
	}
	if presigned.Data.UploadID == "" || presigned.Data.URL == "" || presigned.Data.Method != http.MethodPut {
		t.Fatalf("presign payload = %#v", presigned.Data)
	}
	completed := request(http.MethodPost, "/api/v1/uploads/"+presigned.Data.UploadID+"/complete", token, `{}`, "https://api.example")
	if completed.Code != http.StatusAccepted || !strings.Contains(completed.Body.String(), `"status":"scan_queued"`) {
		t.Fatalf("complete: %d %s", completed.Code, completed.Body.String())
	}
	status := request(http.MethodGet, "/api/v1/uploads/"+presigned.Data.UploadID, token, "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"scan_queued"`) {
		t.Fatalf("owner status: %d %s", status.Code, status.Body.String())
	}
	if response := request(http.MethodGet, "/api/v1/uploads/"+presigned.Data.UploadID, otherToken, "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("other member upload status: %d %s", response.Code, response.Body.String())
	}
	if owner == "" {
		t.Fatal("test owner not created")
	}
}

func TestUploadRoutesRequireBearerAuthenticationInOpenAPI(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	var document struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	for path, method := range map[string]string{
		"/api/v1/uploads/presign":             "post",
		"/api/v1/uploads/{uploadId}/complete": "post",
		"/api/v1/uploads/{uploadId}":          "get",
	} {
		operation := document.Paths[path][method]
		security, ok := operation["security"].([]any)
		if len(security) != 1 || !ok {
			t.Errorf("%s %s must require bearer authentication", method, path)
		}
	}
}

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestProfilePhotoRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("a", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	create := func(email string) (string, auth.SessionTokens) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Photo','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens
	}
	owner, ownerTokens := create("photo-owner@example.invalid")
	_, otherTokens := create("photo-other@example.invalid")
	if _, err := db.Exec(`UPDATE accounts SET directory_visible=true WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	var uploadID string
	if err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES(gen_random_uuid(),$1,'profile_photo','profiles/source.png','available/profile-photo.png','image/png',128,'image/png',128,'available',now()+interval '1 hour',now(),now()) RETURNING id::text`, owner).Scan(&uploadID); err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://app.example", MediaStorage: &testMediaStorage{}})
	request := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	if res := request(http.MethodPut, "/api/v1/me/avatar", `{"uploadId":"`+uploadID+`"}`, ownerTokens.AccessToken, ""); res.Code != http.StatusForbidden {
		t.Fatalf("missing Origin: %d %s", res.Code, res.Body.String())
	}
	if res := request(http.MethodPut, "/api/v1/me/avatar", `{"uploadId":"`+uploadID+`"}`, otherTokens.AccessToken, "https://app.example"); res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("another member's photo accepted: %d %s", res.Code, res.Body.String())
	}
	set := request(http.MethodPut, "/api/v1/me/avatar", `{"uploadId":"`+uploadID+`"}`, ownerTokens.AccessToken, "https://app.example")
	if set.Code != http.StatusOK || !strings.Contains(set.Body.String(), `"avatar":{"id":"`+uploadID+`"`) || !strings.Contains(set.Body.String(), "https://r2.example.invalid/available/profile-photo.png") || strings.Contains(set.Body.String(), "objectKey") {
		t.Fatalf("set own photo: %d %s", set.Code, set.Body.String())
	}
	listed := request(http.MethodGet, "/api/v1/members", "", otherTokens.AccessToken, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"avatar":{"id":"`+uploadID+`"`) {
		t.Fatalf("visible member photo: %d %s", listed.Code, listed.Body.String())
	}
	if res := request(http.MethodGet, "/api/v1/me", "", ownerTokens.AccessToken, ""); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"avatar":{"id":"`+uploadID+`"`) {
		t.Fatalf("own photo read: %d %s", res.Code, res.Body.String())
	}
	removed := request(http.MethodDelete, "/api/v1/me/avatar", "", ownerTokens.AccessToken, "https://app.example")
	if removed.Code != http.StatusOK || strings.Contains(removed.Body.String(), `"avatar":`) {
		t.Fatalf("remove photo: %d %s", removed.Code, removed.Body.String())
	}
	var state string
	if err := db.QueryRow(`SELECT status FROM media_uploads WHERE id=$1`, uploadID).Scan(&state); err != nil || state != media.UploadDeleting {
		t.Fatalf("removed photo status=%q err=%v; want deleting", state, err)
	}
}

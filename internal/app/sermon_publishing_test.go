package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/sermons"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestSermonPublishingThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("k", 32))
	origin := "https://api.example"
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(role string) (string, auth.SessionTokens) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Test','Editor',$1) RETURNING id`, now).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, id, role); err != nil {
				t.Fatal(err)
			}
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens
	}
	staff, editor := actor("content_editor")
	_, member := actor("")
	storage := &testMediaStorage{info: media.ObjectInfo{ContentLength: 32, ContentType: "image/png"}}
	handler := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }, MediaStorage: storage})
	request := func(method, path, token, body, from string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", "application/json")
		if from != "" {
			r.Header.Set("Origin", from)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// Execute the actual Swagger examples so placeholders cannot silently break Try it out.
	specResponse := request("GET", "/openapi.json", "", "", "")
	var spec map[string]any
	if err := json.Unmarshal(specResponse.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	example := func(path, method, name string) string {
		op := paths[path].(map[string]any)[method].(map[string]any)
		examples := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["examples"].(map[string]any)
		raw, err := json.Marshal(examples[name].(map[string]any)["value"])
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	sample := request("POST", "/api/v1/admin/sermons", editor.AccessToken, example("/api/v1/admin/sermons", "post", "draftWithoutVideo"), origin)
	if sample.Code != 201 {
		t.Fatalf("Swagger create example failed: %d %s", sample.Code, sample.Body.String())
	}
	var sampleDraft struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(sample.Body.Bytes(), &sampleDraft); err != nil {
		t.Fatal(err)
	}
	sample = request("PATCH", "/api/v1/admin/sermons/"+sampleDraft.Data.ID, editor.AccessToken, example("/api/v1/admin/sermons/{sermonId}", "patch", "publish"), origin)
	if sample.Code != 200 {
		t.Fatalf("Swagger publish example failed: %d %s", sample.Code, sample.Body.String())
	}
	var sermonMediaID string
	if err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES(gen_random_uuid(),$1,'sermon_attachment','sermons/source.png','available/sermon/clean.png','image/png',32,'image/png',32,'available',now()+interval '1 hour',now(),now()) RETURNING id::text`, staff).Scan(&sermonMediaID); err != nil {
		t.Fatal(err)
	}
	body := `{"title":"Faith","speaker":"Pastor Joy","summary":"A message","preachedAt":"2026-10-11T14:00:00Z","videoUrl":"https://youtu.be/dQw4w9WgXcQ","imageUrl":"https://images.unsplash.com/photo-1770097286875-0cbf4ca2f8c1?auto=format&fit=crop&w=1200&q=80","attachmentIds":["` + sermonMediaID + `"]}`
	for _, tc := range []struct {
		token, origin string
		want          int
	}{{"", origin, 401}, {member.AccessToken, origin, 403}, {editor.AccessToken, "", 403}, {editor.AccessToken, "https://foreign.example", 403}} {
		w := request("POST", "/api/v1/admin/sermons", tc.token, body, tc.origin)
		if w.Code != tc.want {
			t.Fatalf("authorization: %d want %d: %s", w.Code, tc.want, w.Body.String())
		}
	}
	for _, tc := range []struct {
		body, field string
		status      int
	}{
		{`{"title":"Faith","speaker":"Speaker","preachedAt":"2026-10-17T12:00:00Z","videoUrl":"string"}`, "videoUrl", 422},
		{`{"title":"Faith","speaker":"Speaker","preachedAt":"2026-10-17T12:00:00Z","imageUrl":"https://evil.example/photo-123"}`, "imageUrl", 422},
		{`{"id":"response-only"}`, "id", 400},
	} {
		w := request("POST", "/api/v1/admin/sermons", editor.AccessToken, tc.body, origin)
		var e struct {
			Error struct {
				Fields map[string]string `json:"fields"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || e.Error.Fields[tc.field] == "" {
			t.Fatalf("missing actionable %s error: %d %s", tc.field, w.Code, w.Body.String())
		}
	}
	w := request("POST", "/api/v1/admin/sermons", editor.AccessToken, body, origin)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data sermons.EditorialSermon `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.Status != "draft" || len(result.Data.AttachmentIDs) != 1 || result.Data.AttachmentIDs[0] != sermonMediaID {
		t.Fatalf("draft response: %s", w.Body.String())
	}
	id := result.Data.ID
	if w = request("GET", "/api/v1/sermons/"+id, "", "", ""); w.Code != 404 {
		t.Fatal("draft leaked")
	}
	if w = request("GET", "/api/v1/admin/sermons/"+id, editor.AccessToken, "", origin); w.Code != 200 {
		t.Fatal("editor cannot read draft")
	}
	if w = request("GET", "/api/v1/admin/sermons/"+id, member.AccessToken, "", origin); w.Code != 403 {
		t.Fatal("member can read draft")
	}
	if w = request("PATCH", "/api/v1/admin/sermons/"+id, editor.AccessToken, `{"version":1,"status":"published"}`, origin); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/sermons/"+id, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "youtube.com/watch?v=") || !strings.Contains(w.Body.String(), "images.unsplash.com/photo-1770097286875-0cbf4ca2f8c1") || !strings.Contains(w.Body.String(), "r2.example.invalid/available/sermon/clean.png") {
		t.Fatal("published media unavailable")
	}
	if w = request("PATCH", "/api/v1/admin/sermons/"+id, editor.AccessToken, `{"version":1,"title":"Stale"}`, origin); w.Code != 409 {
		t.Fatal("stale update accepted")
	}
	for _, payload := range []string{`{"version":2,"title":""}`, `{"version":2,"videoUrl":"javascript:alert(1)"}`, `{"version":2,"status":"scheduled"}`, `{"version":0,"title":"Invalid"}`} {
		w = request("PATCH", "/api/v1/admin/sermons/"+id, editor.AccessToken, payload, origin)
		if w.Code != 422 {
			t.Fatalf("validation: %d %s", w.Code, w.Body.String())
		}
	}
	if w = request("PATCH", "/api/v1/admin/sermons/"+id, editor.AccessToken, `{"version":2,"role":"system_administrator"}`, origin); w.Code != 400 {
		t.Fatal("unknown privilege field accepted")
	}
	r := httptest.NewRequest("OPTIONS", "/api/v1/admin/sermons/"+id, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 || !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatal("PATCH preflight unavailable")
	}

	if _, err = db.Exec(`CREATE FUNCTION reject_http_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private audit storage failure'; END $$; CREATE TRIGGER reject_http_audit BEFORE INSERT ON sermon_audit_events FOR EACH ROW EXECUTE FUNCTION reject_http_audit()`); err != nil {
		t.Fatal(err)
	}
	w = request("PATCH", "/api/v1/admin/sermons/"+id, editor.AccessToken, `{"version":2,"title":"Rollback"}`, origin)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private audit") {
		t.Fatalf("unsafe audit failure: %d %s", w.Code, w.Body.String())
	}
	if _, err = db.Exec(`DROP TRIGGER reject_http_audit ON sermon_audit_events; DROP FUNCTION reject_http_audit()`); err != nil {
		t.Fatal(err)
	}

	limited := false
	for range 61 {
		w = request("POST", "/api/v1/admin/sermons", editor.AccessToken, `{}`, origin)
		if w.Code == 429 {
			limited = true
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing editor retry hint")
			}
			break
		}
		if w.Code != 422 {
			t.Fatalf("unexpected limit probe result: %d", w.Code)
		}
	}
	if !limited {
		t.Fatal("editor write limit was not enforced")
	}
	if w = request("GET", "/api/v1/admin/sermons/"+id, editor.AccessToken, "", origin); w.Code != 200 {
		t.Fatal("write limit blocked editor read")
	}
	_, otherEditor := actor("content_editor")
	if w = request("POST", "/api/v1/admin/sermons", otherEditor.AccessToken, `{}`, origin); w.Code != 422 {
		t.Fatal("another editor inherited the first editor's limit")
	}
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	if w = request("PATCH", "/api/v1/admin/sermons/"+id, editor.AccessToken, `{"version":2,"status":"draft"}`, origin); w.Code != 403 {
		t.Fatal("revoked editor can write")
	}
	if err = sessions.Logout(ctx, editor.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "/api/v1/admin/sermons/"+id, editor.AccessToken, "", origin); w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
}

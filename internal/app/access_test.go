package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestAccessManagementThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("a", 32))
	origin := "https://api.example"
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(role string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Access','Test',now()) RETURNING id`).Scan(&id); err != nil {
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
		return id, tokens.AccessToken
	}
	admin, adminToken := actor("access_administrator")
	target, memberToken := actor("")
	_, systemToken := actor("system_administrator")
	h := New(db, Config{VerificationKey: key, AppOrigin: origin})
	request := func(method, path, token, body, from string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if from != "" {
			r.Header.Set("Origin", from)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/admin/accounts/" + target + "/roles"
	body := `{"role":"content_editor","reason":"Leadership appointment"}`
	for _, tc := range []struct {
		token, from string
		want        int
	}{{"", origin, 401}, {memberToken, origin, 403}, {systemToken, origin, 403}, {adminToken, "", 403}} {
		w := request("POST", path, tc.token, body, tc.from)
		if w.Code != tc.want {
			t.Fatalf("auth: %d want %d %s", w.Code, tc.want, w.Body.String())
		}
	}
	w := request("POST", path, adminToken, body, origin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", path, adminToken, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "content_editor") {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	sermon := `{"title":"Granted editor","speaker":"Speaker","preachedAt":"2026-10-17T12:00:00Z"}`
	if w = request("POST", "/api/v1/admin/sermons", memberToken, sermon, origin); w.Code != 201 {
		t.Fatalf("grant ineffective: %d %s", w.Code, w.Body.String())
	}
	for _, payload := range []string{`{"role":"access_administrator","reason":"Escalate"}`, `{"role":"content_editor","reason":""}`} {
		if w = request("POST", path, adminToken, payload, origin); w.Code != 422 {
			t.Fatalf("invalid: %d %s", w.Code, w.Body.String())
		}
	}
	if w = request("POST", path, adminToken, `{"role":"content_editor","reason":"Reason","actorId":"forged"}`, origin); w.Code != 400 {
		t.Fatal("unknown privilege field accepted")
	}
	if w = request("POST", "/api/v1/admin/accounts/"+admin+"/roles", adminToken, body, origin); w.Code != 403 {
		t.Fatal("self promotion allowed")
	}
	if _, err = db.Exec(`CREATE FUNCTION reject_access_http_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private access storage failure'; END $$; CREATE TRIGGER reject_access_http_audit BEFORE INSERT ON access_audit_events FOR EACH ROW EXECUTE FUNCTION reject_access_http_audit()`); err != nil {
		t.Fatal(err)
	}
	w = request("POST", path+"/revoke", adminToken, body, origin)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private access storage") {
		t.Fatalf("audit failure: %d %s", w.Code, w.Body.String())
	}
	if _, err = db.Exec(`DROP TRIGGER reject_access_http_audit ON access_audit_events; DROP FUNCTION reject_access_http_audit()`); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", path+"/revoke", adminToken, body, origin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/admin/sermons", memberToken, sermon, origin); w.Code != 403 {
		t.Fatal("revocation ineffective with same JWT")
	}
	limited := false
	for range 31 {
		w = request("GET", path, adminToken, "", "")
		if w.Code == 429 {
			limited = true
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing retry hint")
			}
			break
		}
		if w.Code != 200 {
			t.Fatalf("rate check: %d", w.Code)
		}
	}
	if !limited {
		t.Fatal("access management not rate limited")
	}
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", path, adminToken, body, origin); w.Code != 403 {
		t.Fatal("revoked administrator can grant")
	}
}

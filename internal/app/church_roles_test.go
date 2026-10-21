package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pquerna/otp/totp"
)

type churchHTTPFixture struct {
	db       *sql.DB
	handler  http.Handler
	sessions *auth.SessionService
	mfa      *auth.MFAService
	now      time.Time
	origin   string
}

func newChurchHTTPFixture(t *testing.T) *churchHTTPFixture {
	t.Helper()
	f := &churchHTTPFixture{db: testpg.New(t), now: time.Now().UTC().Truncate(time.Second), origin: "https://church.example.invalid"}
	if err := database.Migrate(context.Background(), f.db); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	var err error
	f.sessions, err = auth.NewSessionService(f.db, auth.SessionConfig{Key: key, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.mfa, err = auth.NewMFAService(f.db, f.sessions, key)
	if err != nil {
		t.Fatal(err)
	}
	f.handler = New(f.db, Config{AppOrigin: f.origin, VerificationKey: key, Now: func() time.Time { return f.now }})
	return f
}
func (f *churchHTTPFixture) account(t *testing.T, role string, enrolled bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := f.db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Church','Tester',$1) RETURNING id`, f.now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tokens, err := f.sessions.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if enrolled {
		setup, err := f.mfa.Begin(ctx, tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		code, err := totp.GenerateCode(setup.Secret, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.mfa.Confirm(ctx, tokens.AccessToken, setup.ID, code); err != nil {
			t.Fatal(err)
		}
	}
	if role != "" {
		if _, err = f.db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, id, role); err != nil {
			t.Fatal(err)
		}
	}
	return id, tokens.AccessToken
}
func (f *churchHTTPFixture) request(method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", f.origin)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.0.2.70:2000"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func TestChurchRolesAnonymousRoutes(t *testing.T) {
	f := newChurchHTTPFixture(t)
	for _, path := range []string{"/api/v1/admin/role-candidates?query=Church", "/api/v1/admin/accounts/11111111-1111-4111-8111-111111111111/church-roles"} {
		if w := f.request("GET", path, "", ""); w.Code != 401 {
			t.Fatalf("missing auth boundary %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
func TestChurchRoleManagementThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	super, superToken := f.account(t, "super_administrator", true)
	_, staffToken := f.account(t, "staff", true)
	_, legacyToken := f.account(t, "access_administrator", true)
	target, targetToken := f.account(t, "", true)
	noMFA, _ := f.account(t, "", false)
	path := "/api/v1/admin/accounts/" + target + "/church-roles"
	body := `{"role":"assistant_pastor","reason":"Approved operational appointment"}`
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {targetToken, 403}, {legacyToken, 403}, {staffToken, 403}} {
		if w := f.request("POST", path, tc.token, body); w.Code != tc.status {
			t.Fatalf("authorization %d want%d %s", w.Code, tc.status, w.Body.String())
		}
	}
	if w := f.request("POST", path, superToken, body); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("grant %d %s", w.Code, w.Body.String())
	}
	if w := f.request("POST", path, superToken, body); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":false`) {
		t.Fatal("duplicate grant", w.Code, w.Body.String())
	}
	if w := f.request("GET", path, staffToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "assistant_pastor") {
		t.Fatal("list", w.Code, w.Body.String())
	}
	if w := f.request("GET", "/api/v1/me/access", targetToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "roles.operational.manage") {
		t.Fatal("same token access", w.Code, w.Body.String())
	}
	if w := f.request("POST", "/api/v1/admin/accounts/"+noMFA+"/church-roles", superToken, body); w.Code != 422 {
		t.Fatal("unenrolled target", w.Code, w.Body.String())
	}
	if w := f.request("POST", "/api/v1/admin/accounts/"+super+"/church-roles", superToken, body); w.Code != 403 {
		t.Fatal("self promotion", w.Code, w.Body.String())
	}
	if w := f.request("POST", path, superToken, `{"role":"staff","reason":"Unknown fields","actorId":"forged"}`); w.Code != 400 {
		t.Fatal("unknown fields", w.Code, w.Body.String())
	}
	if w := f.request("POST", path+"?override=true", superToken, body); w.Code != 422 {
		t.Fatal("query accepted", w.Code, w.Body.String())
	}
	if w := f.request("POST", path+"/revoke", superToken, body); w.Code != 200 {
		t.Fatal("revoke", w.Code, w.Body.String())
	}
	if w := f.request("GET", "/api/v1/me/access", targetToken, ""); w.Code != 200 || strings.Contains(w.Body.String(), "assistant_pastor") {
		t.Fatal("revocation", w.Code, w.Body.String())
	}
}
func TestChurchRolesRequireMandatoryMFAAndFreshIdentity(t *testing.T) {
	f := newChurchHTTPFixture(t)
	admin, token := f.account(t, "super_administrator", true)
	target, _ := f.account(t, "", true)
	path := "/api/v1/admin/accounts/" + target + "/church-roles"
	body := `{"role":"staff","reason":"Test mandatory confirmation"}`
	if _, err := f.db.Exec(`DELETE FROM mfa_session_proofs WHERE session_id IN(SELECT id FROM auth_sessions WHERE account_id=$1)`, admin); err != nil {
		t.Fatal(err)
	}
	if w := f.request("POST", path, token, body); w.Code != 403 || !strings.Contains(w.Body.String(), "mfa_required") {
		t.Fatal("MFA bypass", w.Code, w.Body.String())
	}
	f.now = f.now.Add(6 * time.Minute)
	if w := f.request("POST", path, token, body); w.Code != 403 || !strings.Contains(w.Body.String(), "reauthentication_required") {
		t.Fatal("primary proof bypass", w.Code, w.Body.String())
	}
}
func TestChurchRoleCandidatePrivacyAndValidation(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, admin := f.account(t, "super_administrator", true)
	_, member := f.account(t, "", false)
	for _, tc := range []struct {
		path, token string
		status      int
	}{{"?query=Church&limit=1", member, 403}, {"?query=Ch", admin, 422}, {"?query=Church&limit=999", admin, 422}, {"?query=Church&cursor=bad", admin, 422}, {"?query=Church&override=true", admin, 422}} {
		if w := f.request("GET", "/api/v1/admin/role-candidates"+tc.path, tc.token, ""); w.Code != tc.status {
			t.Fatalf("lookupvalidation %d want%d %s", w.Code, tc.status, w.Body.String())
		}
	}
	w := f.request("GET", "/api/v1/admin/role-candidates?query=Church&limit=1", admin, "")
	var result struct {
		Data []map[string]any
		Meta struct {
			NextCursor string `json:"nextCursor"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || len(result.Data) != 1 || result.Meta.NextCursor == "" {
		t.Fatal("pagination", w.Code, w.Body.String(), err)
	}
	for key := range result.Data[0] {
		switch key {
		case "id", "firstName", "lastName", "email", "mfaEnabled":
		default:
			t.Fatal("unexpected private field", key)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("lookup cached")
	}
	for range 35 {
		w = f.request("GET", "/api/v1/admin/role-candidates?query=Church", admin, "")
	}
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("missing account limit", w.Code)
	}
}

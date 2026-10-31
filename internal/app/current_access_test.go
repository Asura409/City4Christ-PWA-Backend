package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestCurrentAccessThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{42}, 32)
	origin := "https://api.example.invalid"
	now := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	sessions, _ := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	create := func(role string) (string, auth.SessionTokens) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Access','Tester',now()) RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := db.Exec("INSERT INTO account_roles(account_id,role) VALUES($1,$2)", id, role); err != nil {
				t.Fatal(err)
			}
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens
	}
	_, admin := create("access_administrator")
	memberID, member := create("")
	_, other := create("content_editor")
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	read := func(token string) access.CurrentAccess {
		t.Helper()
		w := request("GET", "/api/v1/me/access", token, "")
		if w.Code != 200 {
			t.Fatalf("current access status=%d body=%s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("access response is cacheable")
		}
		var out struct{ Data access.CurrentAccess }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(w.Body.String(), "email") || strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), "password") {
			t.Fatal("private authentication fields leaked")
		}
		return out.Data
	}
	got := read(member.AccessToken)
	if got.Roles == nil || got.Capabilities == nil || len(got.Roles) != 0 || len(got.Capabilities) != 0 {
		t.Fatal("member has staff privileges", got)
	}
	for _, token := range []string{"", member.AccessToken + "tampered"} {
		if w := request("GET", "/api/v1/me/access", token, ""); w.Code != 401 {
			t.Fatal("missing/invalid bearer accepted", w.Code)
		}
	}
	for _, query := range []string{"?accountId=" + memberID, "?role=super_administrator", "?access_token=" + member.AccessToken, "?unknown=%zz", "?"} {
		if w := request("GET", "/api/v1/me/access"+query, member.AccessToken, ""); w.Code != 422 {
			t.Fatalf("query accepted status=%d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/me/access", nil)
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: member.RefreshToken})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cookie authorized current access")
	}
	path := "/api/v1/admin/accounts/" + memberID + "/roles"
	body := `{"role":"content_editor","scope":"","reason":"Approved publishing appointment"}`
	if w := request("POST", path, admin.AccessToken, body); w.Code != 200 {
		t.Fatal("grant failed", w.Code, w.Body.String())
	}
	got = read(member.AccessToken)
	if len(got.Roles) != 1 || len(got.Capabilities) != 2 || got.Capabilities[0].Permission != "devotionals.manage" || got.Capabilities[1].Permission != "sermons.manage" {
		t.Fatal("same JWT did not gain capability", got)
	}
	if w := request("POST", path+"/revoke", admin.AccessToken, body); w.Code != 200 {
		t.Fatal("revoke failed", w.Code)
	}
	got = read(member.AccessToken)
	if len(got.Capabilities) != 0 {
		t.Fatal("same JWT retained revoked capability")
	}
	if w := request("POST", "/api/v1/admin/sermons", member.AccessToken, `{"title":"Forbidden","speaker":"Test","preachedAt":"2026-10-17T12:00:00Z"}`); w.Code != 403 {
		t.Fatal("revoked publishing remains authorized", w.Code)
	}
	if got := read(other.AccessToken); len(got.Capabilities) != 2 || got.Capabilities[0].Permission != "devotionals.manage" || got.Capabilities[1].Permission != "sermons.manage" {
		t.Fatal("own role snapshot incorrect", got)
	}
	// Database failures cannot turn into empty/successful privilege snapshots.
	if _, err := db.Exec("ALTER TABLE account_roles RENAME TO unavailable_access_roles"); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/api/v1/me/access", member.AccessToken, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "account_roles") {
		t.Fatal("storage failure exposed or swallowed", w.Code, w.Body.String())
	}
	if _, err := db.Exec("ALTER TABLE unavailable_access_roles RENAME TO account_roles"); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Logout(ctx, other.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/me/access", other.AccessToken, ""); w.Code != 401 {
		t.Fatal("revoked session read access", w.Code)
	}
	limited := false
	for range 61 {
		w = request("GET", "/api/v1/me/access", member.AccessToken, "")
		if w.Code == 429 {
			limited = w.Header().Get("Retry-After") != ""
			break
		}
		if w.Code != 200 {
			t.Fatal("unexpected rate check status", w.Code)
		}
	}
	if !limited {
		t.Fatal("current access not rate limited")
	}
	if _, err := db.Exec("UPDATE accounts SET email_verified_at=NULL WHERE id=$1", memberID); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/me/access", member.AccessToken, ""); w.Code != 401 {
		t.Fatal("unverified account accepted", w.Code)
	}
}

func TestCurrentAccessUnavailableWithoutConfiguration(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/me/access", nil))
	if w.Code != 503 {
		t.Fatalf("unconfigured access status=%d", w.Code)
	}
}

func TestCurrentAccessIPLimitAcrossAccounts(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{43}, 32)
	origin := "https://api.example.invalid"
	sessions, _ := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	tokens := []string{}
	for range 3 {
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Access','IP test',now()) RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		session, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, session.AccessToken)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: origin})
	for i := 0; i < 121; i++ {
		r := httptest.NewRequest("GET", "/api/v1/me/access", nil)
		r.RemoteAddr = "192.0.2.42:1234"
		r.Header.Set("Authorization", "Bearer "+tokens[i%3])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 120 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("request %d: status=%d want=%d", i+1, w.Code, want)
		}
	}
}

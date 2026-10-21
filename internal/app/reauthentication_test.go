package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestReauthenticationRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	key := make([]byte, 32)
	origin := "https://api.example.invalid"
	hash, err := auth.NewPasswordHasher().Hash("synthetic confirmation password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.NewAccountRepository(db).Create(ctx, auth.NewAccount{Email: "confirm@example.invalid", FirstName: "Confirm", LastName: "Tester", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=$2 WHERE id=$1`, a.ID, now); err != nil {
		t.Fatal(err)
	}
	sessions, _ := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	tokens, err := sessions.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }})
	request := func(method, path, body, token, from string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.50:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", from)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	now = now.Add(6 * time.Minute)
	for _, tc := range []struct {
		method, path, body, token, origin string
		status                            int
	}{
		{"GET", "/api/v1/auth/reauthentication", "", "", origin, 401},
		{"POST", "/api/v1/auth/reauthentication/password", `{"password":"anything"}`, "", origin, 401},
		{"GET", "/api/v1/auth/reauthentication?accountId=other", "", tokens.AccessToken, origin, 422},
		{"POST", "/api/v1/auth/reauthentication/password", `{"password":"anything"}`, tokens.AccessToken, "https://evil.example", 403},
		{"POST", "/api/v1/auth/reauthentication/password", `{"password":"anything","accountId":"other"}`, tokens.AccessToken, origin, 400},
		{"POST", "/api/v1/auth/reauthentication/password", `{"password":"incorrect"}`, tokens.AccessToken, origin, 401},
	} {
		w := request(tc.method, tc.path, tc.body, tc.token, tc.origin)
		if w.Code != tc.status {
			t.Fatalf("%s %s got %d want %d: %s", tc.method, tc.path, w.Code, tc.status, w.Body.String())
		}
	}
	w := request("GET", "/api/v1/auth/reauthentication", "", tokens.AccessToken, origin)
	var result struct{ Data auth.ReauthenticationStatus }
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Data.Fresh || !result.Data.PasswordAvailable {
		t.Fatal("stale status", w.Code, w.Body.String())
	}
	w = request("POST", "/api/v1/auth/reauthentication/password", `{"password":"synthetic confirmation password"}`, tokens.AccessToken, origin)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "accessToken") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("confirmation response", w.Code, w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Data.Fresh {
		t.Fatal("not fresh", err)
	}
	for range 8 {
		w = request("POST", "/api/v1/auth/reauthentication/password", `{"password":"wrong"}`, tokens.AccessToken, origin)
	}
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("account limit missing", w.Code)
	}
	if err = sessions.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "/api/v1/auth/reauthentication", "", tokens.AccessToken, origin); w.Code != 401 {
		t.Fatal("revoked status", w.Code)
	}
}

func TestRoleMutationRequiresFreshConfirmation(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	key := make([]byte, 32)
	origin := "https://api.example.invalid"
	hash, err := auth.NewPasswordHasher().Hash("fresh administrator password")
	if err != nil {
		t.Fatal(err)
	}
	var admin, target string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,password_hash) VALUES('admin-fresh@example.invalid','Admin','Fresh',$1,$2) RETURNING id`, now, hash).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('target-fresh@example.invalid','Target','Fresh',$1) RETURNING id`, now).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'access_administrator')`, admin); err != nil {
		t.Fatal(err)
	}
	s, _ := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	tokens, err := s.Start(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }})
	mutate := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/admin/accounts/"+target+"/roles", strings.NewReader(`{"role":"content_editor","reason":"Test fresh confirmation"}`))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	now = now.Add(6 * time.Minute)
	if w := mutate(); w.Code != 403 || !strings.Contains(w.Body.String(), "reauthentication_required") {
		t.Fatal("stale role write", w.Code, w.Body.String())
	}
	if _, err = s.ReauthenticatePassword(ctx, tokens.AccessToken, "fresh administrator password"); err != nil {
		t.Fatal(err)
	}
	if w := mutate(); w.Code != 200 {
		t.Fatal("fresh role write", w.Code, w.Body.String())
	}
}

func TestGoogleReauthenticationRoutesKeepExistingSessionAndCookie(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	key := make([]byte, 32)
	origin := "https://api.example.invalid"
	front := "https://front.example.invalid"
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('route-google@example.invalid','Google','Confirmation',$1) RETURNING id`, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO external_identities(provider,subject,account_id) VALUES('google','route-subject',$1)`, id); err != nil {
		t.Fatal(err)
	}
	sessions, _ := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	tokens, err := sessions.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	p := &reauthRouteProvider{at: now}
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, AllowedOrigins: []string{front}, GoogleProvider: p, Now: func() time.Time { return now }})
	start := func(target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/auth/reauthentication/google", strings.NewReader(`{"returnTo":"`+target+`"}`))
		r.Header.Set("Origin", front)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := start("https://evil.example/auth.html"); w.Code != 422 {
		t.Fatal("open redirect", w.Code)
	}
	w := start(front + "/auth.html")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct{ Data struct{ URL string } }
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(result.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Name != oidcCookieName {
		t.Fatal("unsafe binding")
	}
	callback := "/api/v1/auth/google/callback?state=" + u.Query().Get("state") + "&code=code"
	r := httptest.NewRequest("GET", callback, nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 302 || w.Header().Get("Location") != front+"/auth.html?reauthentication=complete" || strings.Contains(w.Body.String(), "accessToken") {
		t.Fatal("callback", w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == refreshCookieName {
			t.Fatal("confirmation replaced login cookie")
		}
	}
	if err = sessions.RequireFresh(ctx, tokens.AccessToken); err != nil {
		t.Fatal("original access token not fresh", err)
	}
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, r)
	if replay.Code != 400 {
		t.Fatal("callback replay", replay.Code)
	}
}

type reauthRouteProvider struct {
	routeGoogleProvider
	at time.Time
}

func (p *reauthRouteProvider) Exchange(ctx context.Context, code, nonce, verifier string) (auth.GoogleIdentity, error) {
	id, err := p.routeGoogleProvider.Exchange(ctx, code, nonce, verifier)
	id.AuthenticatedAt = p.at
	return id, err
}

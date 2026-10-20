package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestProfilesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("p", 32))
	origin := "https://api.example"
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	create := func(email string) (string, auth.SessionTokens) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,password_hash,email_verified_at) VALUES($1,'Original','Member','private-hash',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens
	}
	id, first := create("first@example.invalid")
	otherID, other := create("second@example.invalid")
	h := New(db, Config{VerificationKey: key, AppOrigin: origin})
	request := func(method, path, body, token, from string) *httptest.ResponseRecorder {
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
	for _, tc := range []struct {
		token, from string
		want        int
	}{{"", origin, 401}, {first.AccessToken, "", 403}, {first.AccessToken, "https://foreign.example", 403}} {
		w := request("PATCH", "/api/v1/me", `{"firstName":"New"}`, tc.token, tc.from)
		if w.Code != tc.want {
			t.Fatalf("authorization: %d want %d %s", w.Code, tc.want, w.Body.String())
		}
	}
	w := request("GET", "/api/v1/me", "", first.AccessToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"directoryVisible":false`) {
		t.Fatalf("private default: %d %s", w.Code, w.Body.String())
	}
	w = request("PATCH", "/api/v1/me", `{"firstName":"  Grace  ","directoryVisible":true}`, first.AccessToken, origin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"firstName":"Grace"`) || !strings.Contains(w.Body.String(), `"directoryVisible":true`) || strings.Contains(w.Body.String(), "private-hash") {
		t.Fatalf("profile update: %d %s", w.Code, w.Body.String())
	}
	for _, field := range []string{"id", "email", "emailVerified", "hasPassword", "password", "role", "accountId"} {
		body := `{"` + field + `":"forged"}`
		w = request("PATCH", "/api/v1/me", body, first.AccessToken, origin)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"unknown_field"`) {
			t.Fatalf("protected field %s: %d %s", field, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{}`, `{"firstName":""}`, `{"firstName":null}`, `{"lastName":"bad\nname"}`} {
		w = request("PATCH", "/api/v1/me", body, first.AccessToken, origin)
		if w.Code != 422 {
			t.Fatalf("validation: %d %s", w.Code, w.Body.String())
		}
	}
	if w = request("PATCH", "/api/v1/me", `{"directoryVisible":"true"}`, first.AccessToken, origin); w.Code != 400 {
		t.Fatal("wrong privacy type accepted")
	}
	if w = request("PATCH", "/api/v1/me/"+otherID, `{"firstName":"Intruder"}`, first.AccessToken, origin); w.Code != 404 {
		t.Fatal("targeted another account")
	}
	w = request("GET", "/api/v1/me", "", other.AccessToken, "")
	if !strings.Contains(w.Body.String(), `"firstName":"Original"`) || !strings.Contains(w.Body.String(), `"directoryVisible":false`) {
		t.Fatal("other profile changed")
	}
	if w = request("PATCH", "/api/v1/me", `{"directoryVisible":false}`, first.AccessToken, origin); w.Code != 200 || !strings.Contains(w.Body.String(), `"directoryVisible":false`) {
		t.Fatal("opt out failed")
	}
	var email, hash string
	var verified bool
	var roles int
	if err = db.QueryRow(`SELECT email,password_hash,email_verified_at IS NOT NULL,(SELECT count(*) FROM account_roles WHERE account_id=$1) FROM accounts WHERE id=$1`, id).Scan(&email, &hash, &verified, &roles); err != nil || email != "first@example.invalid" || hash != "private-hash" || !verified || roles != 0 {
		t.Fatal("profile edit changed protected data")
	}
	limited := false
	for range 31 {
		w = request("PATCH", "/api/v1/me", `{"lastName":"Member"}`, first.AccessToken, origin)
		if w.Code == 429 {
			limited = true
			break
		}
		if w.Code != 200 {
			t.Fatalf("limit probe: %d", w.Code)
		}
	}
	if !limited {
		t.Fatal("profile writes not limited")
	}
	if w = request("GET", "/api/v1/me", "", first.AccessToken, ""); w.Code != 200 {
		t.Fatal("write limit blocks own read")
	}
	if err = sessions.Logout(ctx, first.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if w = request("PATCH", "/api/v1/me", `{"firstName":"Revoked"}`, first.AccessToken, origin); w.Code != 401 {
		t.Fatal("revoked session edited profile")
	}
	var response any
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
}

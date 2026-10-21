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
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pquerna/otp/totp"
)

func TestMFARoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	key := make([]byte, 32)
	origin := "https://api.example.invalid"
	var id, target string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('mfa-http@example.invalid','MFA','HTTP',$1) RETURNING id`, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('mfa-target@example.invalid','Target','HTTP',$1) RETURNING id`, now).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'access_administrator')`, id); err != nil {
		t.Fatal(err)
	}
	s, _ := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	tokens, err := s.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: origin, Now: func() time.Time { return now }})
	request := func(method, path, body, access, from string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", from)
		if access != "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/auth/mfa", "", "", origin); w.Code != 401 {
		t.Fatal("unauthenticated", w.Code)
	}
	if w := request("POST", "/api/v1/auth/mfa/enroll", "{}", tokens.AccessToken, "https://evil.example"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	if w := request("GET", "/api/v1/auth/mfa?accountId=other", "", tokens.AccessToken, origin); w.Code != 422 {
		t.Fatal("target accepted", w.Code)
	}
	w := request("POST", "/api/v1/auth/mfa/enroll", "{}", tokens.AccessToken, origin)
	var setup struct{ Data auth.MFAEnrollment }
	if err = json.Unmarshal(w.Body.Bytes(), &setup); err != nil || w.Code != 200 || setup.Data.Secret == "" {
		t.Fatal("setup", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || len(w.Result().Cookies()) != 0 {
		t.Fatal("unsafe setup response")
	}
	code, err := totp.GenerateCode(setup.Data.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"enrollmentId":"` + setup.Data.ID + `","code":"` + code + `"}`
	w = request("POST", "/api/v1/auth/mfa/confirm", body, tokens.AccessToken, origin)
	var recovery struct {
		Data struct{ RecoveryCodes []string }
	}
	if err = json.Unmarshal(w.Body.Bytes(), &recovery); err != nil || w.Code != 200 || len(recovery.Data.RecoveryCodes) != 10 {
		t.Fatal("confirm", w.Code, w.Body.String())
	}
	w = request("GET", "/api/v1/auth/mfa", "", tokens.AccessToken, origin)
	if w.Code != 200 || strings.Contains(w.Body.String(), setup.Data.Secret) || strings.Contains(w.Body.String(), recovery.Data.RecoveryCodes[0]) {
		t.Fatal("status leaked secrets", w.Code, w.Body.String())
	}
	rolePath := "/api/v1/admin/accounts/" + target + "/roles"
	roleBody := `{"role":"content_editor","reason":"Synthetic MFA check"}`
	if w = request("POST", rolePath, roleBody, second.AccessToken, origin); w.Code != 403 || !strings.Contains(w.Body.String(), "mfa_required") {
		t.Fatal("role mutation bypassed MFA", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/auth/mfa/recovery", `{"recoveryCode":"`+recovery.Data.RecoveryCodes[0]+`"}`, second.AccessToken, origin); w.Code != 200 {
		t.Fatal("recover", w.Code, w.Body.String())
	}
	if w = request("POST", rolePath, roleBody, second.AccessToken, origin); w.Code != 200 {
		t.Fatal("confirmed role denied", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/auth/mfa/recovery", `{"recoveryCode":"`+recovery.Data.RecoveryCodes[0]+`"}`, second.AccessToken, origin); w.Code != 400 {
		t.Fatal("recovery replay", w.Code)
	}
	for range 6 {
		w = request("POST", "/api/v1/auth/mfa/verify", `{"code":"000000"}`, second.AccessToken, origin)
	}
	if w.Code != 429 {
		t.Fatal("attempt limit", w.Code)
	}
}

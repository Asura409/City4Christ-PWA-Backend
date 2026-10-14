package main

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestProtectedSameOriginPreviewThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	cfg := config{origin: "https://preview.example", password: "test-only-secret", key: make([]byte, 32)}
	h := handler(db, cfg)
	request := func(method, path, body, origin string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if authenticated {
			r.SetBasicAuth("preview", cfg.password)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/auth-test", "", "", false); w.Code != 401 {
		t.Fatalf("unprotected preview: %d", w.Code)
	}
	if w := request("GET", "/auth-test", "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), "width=device-width") {
		t.Fatalf("mobile page: %d", w.Code)
	}
	signup := `{"firstName":"Preview","lastName":"Test","email":"preview@example.invalid","password":"only a temporary test password"}`
	if w := request("POST", "/api/v1/auth/register", signup, "https://other.example", true); w.Code != 403 {
		t.Fatalf("cross origin accepted: %d", w.Code)
	}
	if w := request("POST", "/api/v1/auth/register", signup, "", true); w.Code != 403 {
		t.Fatalf("missing origin accepted: %d", w.Code)
	}
	if w := request("POST", "/api/v1/auth/register", signup, cfg.origin, true); w.Code != 201 {
		t.Fatalf("registration: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/auth-test/inbox?email=preview@example.invalid", "", "", false); w.Code != 401 {
		t.Fatalf("unprotected inbox: %d", w.Code)
	}
	if w := request("GET", "/auth-test/inbox?email=preview@example.invalid", "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"code"`) {
		t.Fatalf("inbox: %d %s", w.Code, w.Body.String())
	} else if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("inbox cached")
	}
}

func TestConfigRequiresHTTPSOriginAndSecrets(t *testing.T) {
	env := map[string]string{"APP_ORIGIN": "https://test.trycloudflare.com", "PREVIEW_PASSWORD": "a-long-test-preview-secret", "DATABASE_URL": "postgres://test@localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString(make([]byte, 32))}
	for _, bad := range []string{"", "http://test.example", "https://test.example/path", "https://user:pass@test.example", "https://test.example?query=1"} {
		env["APP_ORIGIN"] = bad
		if _, err := loadConfig(func(k string) string { return env[k] }); err == nil {
			t.Fatalf("accepted origin %q", bad)
		}
	}
	env["APP_ORIGIN"] = "https://test.trycloudflare.com"
	if _, err := loadConfig(func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewBearerRoutesUseSessionAuthentication(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	h := handler(db, config{origin: "https://preview.example", password: "preview-secret", key: make([]byte, 32)})
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.Header.Set("Authorization", "Bearer invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "invalid_session") || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("preview gate hides bearer auth: %d %s", w.Code, w.Body.String())
	}
}

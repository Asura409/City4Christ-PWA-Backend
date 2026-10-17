package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationThroughGlobalApp(t *testing.T) {
	handler := New(nil, Config{})
	for _, path := range []string{"/docs/", "/docs/swagger-ui.css", "/docs/swagger-ui-bundle.js", "/docs/init.js", "/openapi.json"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if path == "/openapi.json" {
			var spec struct {
				OpenAPI string                     `json:"openapi"`
				Paths   map[string]json.RawMessage `json:"paths"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/api/v1/admin/accounts/{accountId}/roles", "/api/v1/admin/accounts/{accountId}/roles/revoke", "/api/v1/admin/sermons", "/api/v1/admin/sermons/{sermonId}", "/api/v1/sermons", "/api/v1/sermons/{sermonId}", "/health/live", "/health/ready", "/api/v1/auth/register", "/api/v1/auth/verification/verify", "/api/v1/auth/verification/resend", "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/logout", "/api/v1/me", "/api/v1/auth/google/start", "/api/v1/auth/google/link", "/api/v1/auth/google/callback"} {
				if _, ok := spec.Paths[route]; !ok {
					t.Fatalf("missing contract: %s", route)
				}
			}
		}
		if path == "/docs/init.js" && (!strings.Contains(w.Body.String(), "withCredentials: true") || !strings.Contains(w.Body.String(), "validatorUrl: null")) {
			t.Fatal("missing same-origin cookie or private validator configuration")
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code != 308 || w.Header().Get("Location") != "/docs/" {
		t.Fatal("docs redirect missing")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/docs/not-found", nil))
	if w.Code != 404 {
		t.Fatal("unknown docs asset exposed")
	}
}

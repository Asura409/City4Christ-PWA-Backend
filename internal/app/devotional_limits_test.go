package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevotionalOriginsAndPublicRateLimit(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, editor := f.account(t, "content_editor", false)
	r := httptest.NewRequest("POST", "/api/v1/admin/devotionals", strings.NewReader(`{"title":"Hope","body":"Reflection"}`))
	r.Header.Set("Authorization", "Bearer "+editor)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
	limited := false
	for range 121 {
		w = f.request("GET", "/api/v1/devotionals", "", "")
		if w.Code == 429 {
			limited = true
			break
		}
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if !limited {
		t.Fatal("public rate limit missing")
	}
}

package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDailyVerseOriginAndSharedReadQuota(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, editor := f.account(t, "content_editor", false)
	r := httptest.NewRequest("POST", "/api/v1/admin/daily-verses", strings.NewReader(`{"displayDate":"2026-11-01","text":"Test","scriptureReference":"Test 1:1"}`))
	r.Header.Set("Authorization", "Bearer "+editor)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
	for i := 0; i < 120; i++ {
		path := "/api/v1/devotionals"
		if i%2 == 0 {
			path = "/api/v1/daily-verse"
		}
		if w = f.request("GET", path, "", ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w = f.request("GET", "/api/v1/daily-verse", "", ""); w.Code != 429 {
		t.Fatal("shared read quota bypassed", w.Code)
	}
}

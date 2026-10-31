package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/devotionals"
	"strings"
	"testing"
	"time"
)

func TestDailyVerseThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	f.now = time.Date(2026, 10, 31, 22, 59, 59, 0, time.UTC)
	editorID, editor := f.account(t, "content_editor", false)
	_, member := f.account(t, "", false)
	today := pastoralData[devotionals.TodayVerse](t, f.request("GET", "/api/v1/daily-verse", "", ""), 200)
	if today.Date != "2026-10-31" || today.Verse != nil {
		t.Fatal(today)
	}
	body := `{"displayDate":"2026-11-01","text":"Synthetic verse text.","scriptureReference":"Test 1:1","translation":"Test translation"}`
	if w := f.request("POST", "/api/v1/admin/daily-verses", "", body); w.Code != 401 {
		t.Fatal("anonymous editor", w.Code)
	}
	if w := f.request("POST", "/api/v1/admin/daily-verses", member, body); w.Code != 403 {
		t.Fatal("member edited", w.Code)
	}
	if w := f.request("POST", "/api/v1/admin/daily-verses", editor, `{"displayDate":"2026-11-01","text":" ","scriptureReference":"Test 1:1"}`); w.Code != 422 || !strings.Contains(w.Body.String(), `"text":`) {
		t.Fatal("field validation", w.Code, w.Body.String())
	}
	if w := f.request("POST", "/api/v1/admin/daily-verses", editor, `{"displayDate":"2026-11-01","text":"Test","scriptureReference":"Test 1:1","timeZone":"UTC"}`); w.Code != 400 {
		t.Fatal("client timezone override", w.Code)
	}
	v := pastoralData[devotionals.Verse](t, f.request("POST", "/api/v1/admin/daily-verses", editor, body), 201)
	if v.Status != "draft" || v.Version != 1 {
		t.Fatal(v)
	}
	if w := f.request("GET", "/api/v1/admin/daily-verses/"+v.ID, member, ""); w.Code != 403 {
		t.Fatal("private draft leaked", w.Code)
	}
	_ = pastoralData[devotionals.Verse](t, f.request("GET", "/api/v1/admin/daily-verses/"+v.ID, editor, ""), 200)
	page := pastoralData[[]devotionals.Verse](t, f.request("GET", "/api/v1/admin/daily-verses", editor, ""), 200)
	if len(page) != 1 {
		t.Fatal("editor queue", page)
	}
	_ = pastoralData[devotionals.Verse](t, f.request("PATCH", "/api/v1/admin/daily-verses/"+v.ID, editor, `{"version":1,"status":"published"}`), 200)
	today = pastoralData[devotionals.TodayVerse](t, f.request("GET", "/api/v1/daily-verse", "", ""), 200)
	if today.Verse != nil {
		t.Fatal("future verse leaked", today)
	}
	for _, q := range []string{"?date=2026-11-01", "?timeZone=UTC", "?", "?date=a&date=b"} {
		if w := f.request("GET", "/api/v1/daily-verse"+q, "", ""); w.Code != 422 {
			t.Fatal("public date override", q, w.Code)
		}
	}
	f.now = f.now.Add(time.Second)
	w := f.request("GET", "/api/v1/daily-verse", "", "")
	today = pastoralData[devotionals.TodayVerse](t, w, 200)
	if today.Date != "2026-11-01" || today.Verse == nil || today.Verse.ID != v.ID || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(today)
	}
	for _, field := range []string{`"version"`, `"status"`, `"createdAt"`, `"publishedAt"`} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatal("editorial metadata leaked", field)
		}
	}
	second := pastoralData[devotionals.Verse](t, f.request("POST", "/api/v1/admin/daily-verses", editor, body), 201)
	if w = f.request("PATCH", "/api/v1/admin/daily-verses/"+second.ID, editor, `{"version":1,"status":"published"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "verse_date_conflict") {
		t.Fatal("same-day replacement", w.Code, w.Body.String())
	}
	if w = f.request("PATCH", "/api/v1/admin/daily-verses/"+v.ID, editor, `{"version":1,"status":"draft"}`); w.Code != 409 {
		t.Fatal("stale version", w.Code)
	}
	_ = pastoralData[devotionals.Verse](t, f.request("PATCH", "/api/v1/admin/daily-verses/"+v.ID, editor, `{"version":2,"status":"draft"}`), 200)
	today = pastoralData[devotionals.TodayVerse](t, f.request("GET", "/api/v1/daily-verse", "", ""), 200)
	if today.Verse != nil {
		t.Fatal("withdrawn verse visible")
	}
	if _, err := f.db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, editorID); err != nil {
		t.Fatal(err)
	}
	if w = f.request("POST", "/api/v1/admin/daily-verses", editor, body); w.Code != 403 {
		t.Fatal("revoked editor", w.Code)
	}
}

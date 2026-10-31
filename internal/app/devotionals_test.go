package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/devotionals"
	"strings"
	"testing"
)

func TestDevotionalPublishingThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	editorID, editor := f.account(t, "content_editor", false)
	_, member := f.account(t, "", false)
	body := `{"title":"Hope today","body":"Reflect and pray.","scriptureReference":"Psalm 28:7","author":"City4Christ"}`
	if w := f.request("POST", "/api/v1/admin/devotionals", "", body); w.Code != 401 {
		t.Fatalf("anonymous create %d", w.Code)
	}
	if w := f.request("POST", "/api/v1/admin/devotionals", member, body); w.Code != 403 {
		t.Fatal("member created", w.Code)
	}
	if w := f.request("POST", "/api/v1/admin/devotionals", editor, `{"title":"Hope","body":""}`); w.Code != 422 || !strings.Contains(w.Body.String(), `"body":`) {
		t.Fatal("field error missing", w.Code, w.Body.String())
	}
	if w := f.request("POST", "/api/v1/admin/devotionals", editor, `{"title":"Hope","body":"text","publishedAt":"2026-11-01T10:00:00Z"}`); w.Code != 400 {
		t.Fatal("forged publication date", w.Code)
	}
	d := pastoralData[devotionals.Entry](t, f.request("POST", "/api/v1/admin/devotionals", editor, body), 201)
	if d.Status != "draft" || d.Version != 1 {
		t.Fatal(d)
	}
	if w := f.request("GET", "/api/v1/devotionals/"+d.ID, "", ""); w.Code != 404 {
		t.Fatal("draft visible", w.Code)
	}
	if w := f.request("GET", "/api/v1/admin/devotionals/"+d.ID, member, ""); w.Code != 403 {
		t.Fatal("draft read", w.Code)
	}
	page := pastoralData[[]devotionals.PublishedEntry](t, f.request("GET", "/api/v1/devotionals", "", ""), 200)
	if len(page) != 0 {
		t.Fatal("draft listed")
	}
	list := pastoralData[[]devotionals.Entry](t, f.request("GET", "/api/v1/admin/devotionals", editor, ""), 200)
	if len(list) != 1 {
		t.Fatal("editor queue missing")
	}
	p := pastoralData[devotionals.Entry](t, f.request("PATCH", "/api/v1/admin/devotionals/"+d.ID, editor, `{"version":1,"status":"published"}`), 200)
	if p.Version != 2 {
		t.Fatal(p)
	}
	w := f.request("GET", "/api/v1/devotionals/"+d.ID, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"version"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public DTO/cache", w.Code, w.Body.String())
	}
	if w = f.request("PATCH", "/api/v1/admin/devotionals/"+d.ID, editor, `{"version":1,"status":"draft"}`); w.Code != 409 {
		t.Fatal("stale writer", w.Code)
	}
	for _, q := range []string{"?limit=0", "?limit=1&limit=2", "?status=draft", "?cursor=bad"} {
		if w = f.request("GET", "/api/v1/devotionals"+q, "", ""); w.Code != 422 {
			t.Fatal("bad query", q, w.Code)
		}
	}
	if w = f.request("POST", "/api/v1/admin/devotionals?ignored=1", editor, body); w.Code != 422 {
		t.Fatal("ignored write query", w.Code)
	}
	if w = f.request("PATCH", "/api/v1/admin/devotionals/"+d.ID, editor, `{"version":2,"status":"draft"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/devotionals/"+d.ID, "", ""); w.Code != 404 {
		t.Fatal("withdrawal leaked", w.Code)
	}
	if _, err := f.db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, editorID); err != nil {
		t.Fatal(err)
	}
	if w = f.request("POST", "/api/v1/admin/devotionals", editor, body); w.Code != 403 {
		t.Fatal("revoked token retained authority", w.Code)
	}
}

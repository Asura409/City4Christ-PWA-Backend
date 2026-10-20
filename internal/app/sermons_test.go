package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/sermons"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestSermonCatalogThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	published := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	insert := func(title string, at *time.Time) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO sermons(title,summary,speaker,series,topic,preached_at,published_at) VALUES($1,'Summary','Pastor Joy','Foundations','Faith',$2,$3) RETURNING id`, title, published, at).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	firstID := insert("Faith today", &published)
	insert("Faith tomorrow", &published)
	draftID := insert("Secret draft", nil)
	futureID := insert("Future message", &future)
	handler := New(db, Config{Now: func() time.Time { return now }, AppOrigin: "https://api.example"})
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	w := get("/api/v1/sermons?limit=1&query=faith&speaker=pastor%20joy&series=foundations&topic=faith")
	if w.Code != 200 {
		t.Fatalf("public list: %d %s", w.Code, w.Body.String())
	}
	var page struct {
		Data []sermons.Sermon `json:"data"`
		Meta struct {
			NextCursor string `json:"nextCursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Data) != 1 || page.Meta.NextCursor == "" {
		t.Fatalf("first page: %s", w.Body.String())
	}
	firstPageID := page.Data[0].ID
	w = get("/api/v1/sermons?limit=1&cursor=" + page.Meta.NextCursor)
	if w.Code != 200 {
		t.Fatal("next page unavailable")
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Data) != 1 || page.Data[0].ID == firstPageID || page.Meta.NextCursor != "" {
		t.Fatalf("last page: %s", w.Body.String())
	}
	w = get("/api/v1/sermons/" + firstID)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Faith today") {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
	for _, id := range []string{draftID, futureID, "invalid", "00000000-0000-0000-0000-000000000000"} {
		w = get("/api/v1/sermons/" + id)
		if w.Code != 404 || strings.Contains(w.Body.String(), "Secret") {
			t.Fatalf("private/missing detail: %d", w.Code)
		}
	}
	for _, query := range []string{"limit=0", "limit=-1", "limit=101", "limit=abc", "limit=1&limit=2", "cursor=bad", "status=draft", "query=%FF", "query=%ZZ"} {
		w = get("/api/v1/sermons?" + query)
		if w.Code != 422 {
			t.Fatalf("query %s: %d %s", query, w.Code, w.Body.String())
		}
	}
	w = get("/api/v1/sermons?query=nothing")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("empty list: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sermons", strings.NewReader(`{}`)))
	if w.Code != 405 {
		t.Fatal("public content creation exposed")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/sermons", "/api/v1/sermons/" + firstID} {
		w = get(path)
		if w.Code != 503 || strings.Contains(w.Body.String(), "sql:") || w.Header().Get("X-Request-Id") == "" {
			t.Fatalf("unsafe storage failure: %d %s", w.Code, w.Body.String())
		}
	}
}

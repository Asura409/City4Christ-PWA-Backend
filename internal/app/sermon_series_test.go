package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPublishedSermonSeriesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	insert := func(title, series string, publishedAt *time.Time) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO sermons(title,summary,speaker,series,topic,preached_at,published_at)
			VALUES($1,'Summary','Pastor', $2,'Faith',$3,$4)`, title, series, now.Add(-24*time.Hour), publishedAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	first := now.Add(-48 * time.Hour)
	latest := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	insert("Faith foundations one", "Foundations", &first)
	insert("Faith foundations two", " Foundations ", &latest)
	insert("Unpublished series", "Future Hope", &future)
	insert("Draft series", "Hidden Draft", nil)
	insert("Standalone sermon", "", &latest)

	handler := New(db, Config{Now: func() time.Time { return now }, AppOrigin: "https://api.example"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sermons/series", nil))
	if w.Code != 200 {
		t.Fatalf("published series list: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Data []struct {
			Name              string    `json:"name"`
			SermonCount       int       `json:"sermonCount"`
			LatestPublishedAt time.Time `json:"latestPublishedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("expected one visible normalized series, got: %s", w.Body.String())
	}
	got := response.Data[0]
	if got.Name != "Foundations" || got.SermonCount != 2 || !got.LatestPublishedAt.Equal(latest) {
		t.Fatalf("incorrect series aggregate: %+v", got)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sermons?series=Foundations", nil))
	if w.Code != 200 {
		t.Fatalf("series filter: %d %s", w.Code, w.Body.String())
	}
	var filtered struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &filtered); err != nil || len(filtered.Data) != 2 {
		t.Fatalf("series selection did not return both normalized rows: %s %v", w.Body.String(), err)
	}
}

package sermons

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func fixture(t *testing.T) (*sql.DB, time.Time) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
}
func insert(t *testing.T, db *sql.DB, title, speaker, series, topic string, published *time.Time) string {
	t.Helper()
	var id string
	err := db.QueryRow(`INSERT INTO sermons(title,summary,speaker,series,topic,preached_at,published_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, title, "Message about "+title, speaker, series, topic, time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC), published).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestRepositoryPublicationAndFilters(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	repo := NewRepository(db)
	past := now.Add(-time.Hour)
	future := now.Add(time.Second)
	public := insert(t, db, "Living in Faith", "Pastor Joy", "Foundations", "Faith", &past)
	draft := insert(t, db, "Private draft", "Pastor Joy", "Foundations", "Faith", nil)
	scheduled := insert(t, db, "Scheduled message", "Pastor Joy", "Foundations", "Faith", &future)
	insert(t, db, "Serving others", "Pastor Lee", "Community", "Service", &past)
	got, err := repo.List(ctx, Options{Limit: 10, Query: "faith", Speaker: "pastor joy", Series: "foundations", Topic: "faith"}, nil, now)
	if err != nil || len(got) != 1 || got[0].ID != public {
		t.Fatalf("filtered published records: %+v %v", got, err)
	}

	all, err := repo.List(ctx, Options{Limit: 100}, nil, now)
	if err != nil || len(all) != 2 {
		t.Fatalf("unfiltered list exposed draft or scheduled content: %+v %v", all, err)
	}
	for _, item := range all {
		if item.ID == draft || item.ID == scheduled {
			t.Fatal("unpublished record in public list")
		}
	}
	for name, filter := range map[string]Options{
		"speaker": {Limit: 10, Speaker: "PASTOR JOY"},
		"series":  {Limit: 10, Series: "FOUNDATIONS"},
		"topic":   {Limit: 10, Topic: "FAITH"},
	} {
		items, err := repo.List(ctx, filter, nil, now)
		if err != nil || len(items) != 1 || items[0].ID != public {
			t.Fatalf("independent %s filter: %+v %v", name, items, err)
		}
	}
	for name, filter := range map[string]Options{
		"speaker exact": {Limit: 10, Speaker: "Pastor"},
		"series exact":  {Limit: 10, Series: "Foundation"},
		"topic exact":   {Limit: 10, Topic: "Fai"},
	} {
		items, err := repo.List(ctx, filter, nil, now)
		if err != nil || len(items) != 0 {
			t.Fatalf("%s must not match substrings: %+v %v", name, items, err)
		}
	}
	boundary := insert(t, db, "Publication boundary", "Speaker", "", "", &now)
	items, err := repo.List(ctx, Options{Limit: 100}, nil, now)
	if err != nil || len(items) != 3 || items[0].ID != boundary {
		t.Fatalf("publication boundary not inclusive: %+v %v", items, err)
	}
	for _, id := range []string{draft, scheduled, "00000000-0000-0000-0000-000000000000"} {
		if _, err := repo.Get(ctx, id, now); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden sermon visible: %v", err)
		}
	}
	got, err = repo.List(ctx, Options{Limit: 10, Query: "%"}, nil, now)
	if err != nil || len(got) != 0 {
		t.Fatalf("search treated as wildcard: %v %v", got, err)
	}
	a, err := repo.Get(ctx, public, now)
	if err != nil || a.Title != "Living in Faith" || a.Summary == "" {
		t.Fatalf("detail: %+v %v", a, err)
	}
}
func TestRepositoryStablePageBoundaryAndMigrationPreservation(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	repo := NewRepository(db)
	published := now.Add(-time.Hour)
	for range 5 {
		insert(t, db, "Same date", "Speaker", "Series", "Topic", &published)
	}
	first, err := repo.List(ctx, Options{Limit: 2}, nil, now)
	if err != nil || len(first) != 3 {
		t.Fatalf("lookahead: %+v %v", first, err)
	}
	next, err := repo.List(ctx, Options{Limit: 2}, &Cursor{PublishedAt: first[1].PublishedAt, ID: first[1].ID}, now)
	if err != nil || len(next) != 3 {
		t.Fatalf("next page: %+v %v", next, err)
	}
	for _, a := range first[:2] {
		for _, b := range next {
			if a.ID == b.ID {
				t.Fatal("duplicate across page boundary")
			}
		}
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	all, err := repo.List(ctx, Options{Limit: 100}, nil, now)
	if err != nil || len(all) != 5 {
		t.Fatal("migration rerun lost sermons")
	}
}

package sermons

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServicePaginationAndValidation(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	published := now.Add(-time.Hour)
	for range 5 {
		insert(t, db, "Faith", "Speaker", "Foundations", "Faith", &published)
	}
	service := NewService(NewRepository(db), func() time.Time { return now })
	o := Options{Limit: 2}
	seen := map[string]bool{}
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		page, err := service.List(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate sermon")
			}
			seen[item.ID] = true
		}
		if pageNumber < 2 && page.NextCursor == "" {
			t.Fatal("missing continuation")
		}
		if pageNumber == 2 && page.NextCursor != "" {
			t.Fatal("unexpected final continuation")
		}
		o.Cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatal("pagination lost records")
	}
	empty, err := service.List(ctx, Options{Query: "absent"})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatalf("empty catalog: %+v %v", empty, err)
	}
	for _, bad := range []Options{{Limit: -1}, {Limit: 101}, {Cursor: "not-json"}, {Cursor: strings.Repeat("x", 2049)}, {Query: strings.Repeat("x", 201)}, {Speaker: strings.Repeat("x", 201)}, {Topic: strings.Repeat("x", 101)}, {Series: strings.Repeat("x", 201)}, {Query: string([]byte{255})}} {
		if _, err := service.List(ctx, bad); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("invalid options accepted: %+v %v", bad, err)
		}
	}
	if _, err := service.Get(ctx, "invalid-id"); !errors.Is(err, ErrNotFound) {
		t.Fatal("invalid ID should not disclose a record")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.List(ctx, Options{}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestServiceListsPublishedSeriesAtConfiguredTime(t *testing.T) {
	db, now := fixture(t)
	publication := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	insert(t, db, "Published title", "Speaker", "Grace", "Faith", &publication)
	insert(t, db, "Scheduled title", "Speaker", "Hope", "Faith", &future)
	service := NewService(NewRepository(db), func() time.Time { return now })

	series, err := service.ListSeries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Name != "Grace" {
		t.Fatalf("service exposed a future series: %+v", series)
	}
}

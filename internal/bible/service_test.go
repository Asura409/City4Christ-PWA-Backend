package bible

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type providerFunc func(context.Context, Query) (Passage, error)

func (f providerFunc) Lookup(ctx context.Context, q Query) (Passage, error) { return f(ctx, q) }
func synthetic(q Query) Passage {
	return Passage{Reference: "John 3:16", Translation: Translations()[0], Text: "Synthetic verse.", Verses: []Verse{{BookID: "JHN", BookName: "John", Chapter: 3, Number: 16, Text: "Synthetic verse."}}, Source: "bible-api.com", SourceURL: "https://bible-api.com/John%203:16?translation=web"}
}
func TestLookupValidatesCachesAndExpires(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	calls, gates := 0, 0
	s := NewService(providerFunc(func(_ context.Context, q Query) (Passage, error) { calls++; return synthetic(q), nil }), func(context.Context) (bool, error) { gates++; return true, nil }, func() time.Time { return now })
	for _, q := range []Query{{Reference: ""}, {Reference: "John\n3:16"}, {Reference: "https://bad.example"}, {Reference: "John 3:16", Translation: "niv"}} {
		if _, err := s.Lookup(context.Background(), q); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad lookup", q, err)
		}
	}
	first, err := s.Lookup(context.Background(), Query{Reference: " John  3:16 ", Translation: "WEB"})
	if err != nil {
		t.Fatal(err)
	}
	first.Verses[0].Text = "Changed by caller"
	again, err := s.Lookup(context.Background(), Query{Reference: "john 3:16", Translation: "web"})
	if err != nil || again.Verses[0].Text != "Synthetic verse." || calls != 1 || gates != 1 {
		t.Fatal("bad cache", calls, gates, err)
	}
	now = now.Add(time.Hour + time.Second)
	if _, err = s.Lookup(context.Background(), Query{Reference: "John 3:16"}); err != nil || calls != 2 {
		t.Fatal("cache did not expire", calls, err)
	}
}
func TestConcurrentLookupUsesOneProviderRequest(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := NewService(providerFunc(func(_ context.Context, q Query) (Passage, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		started <- struct{}{}
		<-release
		return synthetic(q), nil
	}), func(context.Context) (bool, error) { return true, nil }, nil)
	done := make(chan error, 2)
	go func() { _, e := s.Lookup(ctx, Query{Reference: "John 3:16"}); done <- e }()
	<-started
	go func() { _, e := s.Lookup(ctx, Query{Reference: "John 3:16"}); done <- e }()
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("duplicate upstream calls", calls)
	}
}
func TestProviderFailuresNeverBecomeCachedFallbacks(t *testing.T) {
	for _, failure := range []error{ErrUnavailable, ErrRateLimited} {
		calls := 0
		s := NewService(providerFunc(func(context.Context, Query) (Passage, error) { calls++; return Passage{}, failure }), func(context.Context) (bool, error) { return true, nil }, nil)
		for range 2 {
			if _, err := s.Lookup(context.Background(), Query{Reference: "John 3:16"}); !errors.Is(err, failure) {
				t.Fatal(err)
			}
		}
		if calls != 2 {
			t.Fatal("failure cached")
		}
	}
	s := NewService(providerFunc(func(context.Context, Query) (Passage, error) { t.Fatal("quota bypass"); return Passage{}, nil }), func(context.Context) (bool, error) { return false, nil }, nil)
	if _, err := s.Lookup(context.Background(), Query{Reference: "John 3:16"}); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
}

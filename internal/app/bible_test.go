package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/bible"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type appBibleProvider func(context.Context, bible.Query) (bible.Passage, error)

func (f appBibleProvider) Lookup(c context.Context, q bible.Query) (bible.Passage, error) {
	return f(c, q)
}
func testBiblePassage(q bible.Query) bible.Passage {
	var translation bible.Translation
	for _, v := range bible.Translations() {
		if v.ID == q.Translation {
			translation = v
		}
	}
	return bible.Passage{Reference: q.Reference, Translation: translation, Text: "Synthetic Bible passage.", Verses: []bible.Verse{{BookID: "JHN", BookName: "John", Chapter: 3, Number: 16, Text: "Synthetic Bible passage."}}, Source: "bible-api.com", SourceURL: "https://bible-api.com/John%203:16?translation=" + q.Translation}
}
func TestBibleLookupThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	var calls atomic.Int32
	f.handler = New(f.db, Config{VerificationKey: make([]byte, 32), AppOrigin: f.origin, Now: func() time.Time { return f.now }, BibleProvider: appBibleProvider(func(_ context.Context, q bible.Query) (bible.Passage, error) {
		calls.Add(1)
		return testBiblePassage(q), nil
	})})
	catalog := pastoralData[[]bible.Translation](t, f.request("GET", "/api/v1/bible/translations", "", ""), 200)
	if len(catalog) != 3 {
		t.Fatal(catalog)
	}
	w := f.request("GET", "/api/v1/bible/passages?reference=John+3%3A16&translation=web", "", "")
	p := pastoralData[bible.Passage](t, w, 200)
	if p.Text != "Synthetic Bible passage." || p.Translation.ID != "web" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(p)
	}
	_ = pastoralData[bible.Passage](t, f.request("GET", "/api/v1/bible/passages?reference=john+3%3A16&translation=web", "", ""), 200)
	if calls.Load() != 1 {
		t.Fatal("cache miss", calls.Load())
	}
	for _, q := range []string{"?reference=John+3%3A16&translation=niv", "?reference=John+3%3A16&translation=web&translation=asv", "?url=https://bad.example", "?reference=John%0A3%3A16"} {
		if w = f.request("GET", "/api/v1/bible/passages"+q, "", ""); w.Code != 422 {
			t.Fatal(q, w.Code, w.Body.String())
		}
	}
	for i := 1; i <= 11; i++ {
		w = f.request("GET", fmt.Sprintf("/api/v1/bible/passages?reference=John+3%%3A%d&translation=web", i), "", "")
		if w.Code != 200 {
			t.Fatal("early quota", w.Code, w.Body.String())
		}
	}
	w = f.request("GET", "/api/v1/bible/passages?reference=John+3%3A30&translation=web", "", "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("provider quota missing", w.Code)
	}
	if calls.Load() != 12 {
		t.Fatal("provider quota bypass", calls.Load())
	}
}
func TestBibleProviderFailureHasNoSecretOrFallback(t *testing.T) {
	f := newChurchHTTPFixture(t)
	f.handler = New(f.db, Config{VerificationKey: make([]byte, 32), AppOrigin: f.origin, BibleProvider: appBibleProvider(func(context.Context, bible.Query) (bible.Passage, error) {
		return bible.Passage{}, errors.New("private-provider-secret")
	})})
	w := f.request("GET", "/api/v1/bible/passages?reference=John+3%3A16", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "private-provider-secret") || strings.Contains(w.Body.String(), "Synthetic") {
		t.Fatal(w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/v1/bible/translations?unknown=1", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	if rec.Code != 422 {
		t.Fatal("ignored catalog query", rec.Code)
	}
}

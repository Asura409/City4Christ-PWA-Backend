package bible

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHelloAOKJVLookupPreservesVerseMarkers(t *testing.T) {
	provider := NewHelloAO(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://bible.helloao.org/api/eng_kjv/JHN/3.json" {
			t.Fatalf("unexpected endpoint: %s", r.URL)
		}
		body := `{"numberOfVerses":17,"chapter":{"number":3,"content":[{"type":"verse","number":16,"content":["For God so loved the world."]},{"type":"verse","number":17,"content":[{"text":"For God did not send His Son."}]}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	got, err := provider.Lookup(context.Background(), Query{Reference: "John 3:16-17", Translation: "kjv"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != "John 3:16-17" || got.Translation.ID != "kjv" || len(got.Verses) != 2 {
		t.Fatalf("unexpected passage: %#v", got)
	}
	if got.Text != "[16] For God so loved the world.\n[17] For God did not send His Son." {
		t.Fatalf("unexpected text: %q", got.Text)
	}
}

func TestMultiProviderIncludesHelloAOKJV(t *testing.T) {
	provider := NewMultiProvider(NewHelloAO(nil), testCatalogProvider{})
	translations, err := provider.Translations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, translation := range translations {
		if translation.ID == "kjv" && translation.CanImport && translation.License == "Public domain via HelloAO" {
			return
		}
	}
	t.Fatalf("KJV missing from catalog: %#v", translations)
}

type testCatalogProvider struct{}

func (testCatalogProvider) Translations(context.Context) ([]Translation, error) {
	return []Translation{{ID: "web", Name: "World English Bible"}}, nil
}

func (testCatalogProvider) Lookup(context.Context, Query) (Passage, error) {
	return Passage{}, ErrUnavailable
}

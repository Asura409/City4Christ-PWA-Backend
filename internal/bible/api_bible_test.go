package bible

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAPIBibleLoadsConfiguredVersionsAndPassages(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("api-key") != "test-key" {
			t.Fatalf("missing API key header")
		}
		var body string
		switch {
		case strings.HasSuffix(r.URL.Path, "/bibles"):
			body = `{"data":[{"id":"niv-id","abbreviation":"NIV","name":"New International Version"},{"id":"kjv-id","abbreviation":"KJV","name":"King James Version"},{"id":"nkjv-id","abbreviation":"NKJV","name":"New King James Version"},{"id":"amp-id","abbreviation":"AMP","name":"Amplified Bible"}]}`
		case strings.Contains(r.URL.Path, "/chapters/"):
			body = `{"data":{"reference":"John 3","copyright":"Test copyright","content":[{"name":"para","type":"tag","items":[{"name":"verse","type":"tag","attrs":{"sid":"JHN 3:16"},"items":[{"text":"16","type":"text"},{"text":"For God so loved the world.","type":"text","attrs":{"verseId":"JHN.3.16"}}]}]}]}}`
		default:
			body = `{"data":{"reference":"John 3:16","copyright":"Test copyright","content":[{"name":"verse","type":"tag","attrs":{"sid":"JHN 3:16"},"items":[{"text":"16","type":"text"},{"text":"For God so loved the world.","type":"text","attrs":{"verseId":"JHN.3.16"}}]}]}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	provider := NewAPIBible("test-key", client)
	translations, err := provider.Translations(context.Background())
	if err != nil || len(translations) != 4 {
		t.Fatalf("catalog = %#v, error = %v", translations, err)
	}
	passage, err := provider.Lookup(context.Background(), Query{Reference: "John 3:16", Translation: "niv"})
	if err != nil || passage.Translation.ID != "niv" || passage.Verses[0].Text != "For God so loved the world." {
		t.Fatalf("passage = %#v, error = %v", passage, err)
	}
}

package bible

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const sampleResponse = `{"reference":"John 3:16","translation_id":"web","translation_name":"World English Bible","verses":[{"book_id":"JHN","book_name":"John","chapter":3,"verse":16,"text":"Synthetic verse.\n"}]}`

func TestBibleAPIUsesFixedHostAndNormalizesPlainText(t *testing.T) {
	p := NewAPI(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "bible-api.com" || r.URL.Query().Get("translation") != "web" {
			t.Error("unsafe target", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sampleResponse)), Header: make(http.Header)}, nil
	})})
	got, err := p.Lookup(context.Background(), Query{Reference: "John 3:16", Translation: "web"})
	if err != nil || got.Text != "Synthetic verse." || got.Reference != "John 3:16" || !got.Translation.CanImport {
		t.Fatal(got, err)
	}
}
func TestBibleAPIRejectsUnsafeOrMismatchedProviderResponses(t *testing.T) {
	for _, body := range []string{`not json`, strings.Replace(sampleResponse, `"web"`, `"asv"`, 1), strings.Replace(sampleResponse, `"verse":16`, `"verse":0`, 1), `{"reference":"John 3:16","translation_id":"web","verses":[]}`, strings.Repeat("x", 65537)} {
		p := NewAPI(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})})
		if _, err := p.Lookup(context.Background(), Query{Reference: "John 3:16", Translation: "web"}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("invalid upstream accepted", err)
		}
	}
	for _, tc := range []struct {
		status int
		want   error
	}{{400, ErrInvalid}, {404, ErrInvalid}, {429, ErrRateLimited}, {302, ErrUnavailable}, {503, ErrUnavailable}} {
		p := NewAPI(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("private provider error")), Header: make(http.Header)}, nil
		})})
		if _, err := p.Lookup(context.Background(), Query{Reference: "John 3:16", Translation: "web"}); !errors.Is(err, tc.want) {
			t.Fatal(tc.status, err)
		}
	}
}

func TestBibleAPIMakesSingleChapterBookRequestsUnambiguous(t *testing.T) {
	p := NewAPI(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("single_chapter_book_matching") != "indifferent" {
			t.Error("single-chapter lookup still defaults to a single verse")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sampleResponse)), Header: make(http.Header)}, nil
	})})
	if _, err := p.Lookup(context.Background(), Query{Reference: "Jude 1", Translation: "web"}); err != nil {
		t.Fatal(err)
	}
}

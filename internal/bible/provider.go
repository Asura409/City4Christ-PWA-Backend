package bible

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type API struct{ client *http.Client }

func NewAPI(client *http.Client) *API {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	if c.Timeout <= 0 || c.Timeout > 5*time.Second {
		c.Timeout = 5 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &API{client: &c}
}
func plain(s string, max int) bool {
	if strings.TrimSpace(s) == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
func (a *API) Lookup(ctx context.Context, q Query) (Passage, error) {
	q, err := normalize(q)
	if err != nil {
		return Passage{}, err
	}
	u := url.URL{Scheme: "https", Host: "bible-api.com", Path: "/" + q.Reference, RawQuery: url.Values{"translation": []string{q.Translation}, "single_chapter_book_matching": []string{"indifferent"}}.Encode()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Passage{}, ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return Passage{}, ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 200:
	case 400, 404:
		return Passage{}, invalid("reference", "Reference not found. Use a book, chapter and optional verse range.")
	case 429:
		return Passage{}, ErrRateLimited
	default:
		return Passage{}, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 || !utf8.Valid(body) {
		return Passage{}, ErrUnavailable
	}
	var raw struct {
		Reference     string `json:"reference"`
		TranslationID string `json:"translation_id"`
		Verses        []struct {
			BookID   string `json:"book_id"`
			BookName string `json:"book_name"`
			Chapter  int    `json:"chapter"`
			Verse    int    `json:"verse"`
			Text     string `json:"text"`
		} `json:"verses"`
	}
	if err = json.Unmarshal(body, &raw); err != nil || strings.ToLower(raw.TranslationID) != q.Translation || !plain(raw.Reference, 200) || len(raw.Verses) < 1 || len(raw.Verses) > 200 {
		return Passage{}, ErrUnavailable
	}
	t, _ := findTranslation(q.Translation)
	p := Passage{Reference: strings.TrimSpace(raw.Reference), Translation: t, Source: "bible-api.com", SourceURL: u.String(), Verses: []Verse{}}
	texts := []string{}
	type verseKey struct {
		book            string
		chapter, number int
	}
	seen := map[verseKey]bool{}
	for _, v := range raw.Verses {
		if !plain(v.BookID, 8) || !plain(v.BookName, 80) || v.Chapter < 1 || v.Chapter > 200 || v.Verse < 1 || v.Verse > 200 || !plain(v.Text, 30000) {
			return Passage{}, ErrUnavailable
		}
		key := verseKey{v.BookID, v.Chapter, v.Verse}
		if seen[key] {
			return Passage{}, ErrUnavailable
		}
		seen[key] = true
		text := strings.TrimSpace(v.Text)
		p.Verses = append(p.Verses, Verse{v.BookID, v.BookName, v.Chapter, v.Verse, text})
		texts = append(texts, text)
	}
	p.Text = strings.Join(texts, "\n")
	if utf8.RuneCountInString(p.Text) > 30000 {
		return Passage{}, ErrUnavailable
	}
	return p, nil
}

package bible

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const helloAOBaseURL = "https://bible.helloao.org/api"

// HelloAO reads the public-domain English KJV from HelloAO's public JSON API.
// The provider exposes the stable application ID "kjv" so existing frontend
// selections and saved Daily Verse records remain compatible.
type HelloAO struct{ client *http.Client }

func NewHelloAO(client *http.Client) *HelloAO {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	if c.Timeout <= 0 || c.Timeout > 5*time.Second {
		c.Timeout = 5 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HelloAO{client: &c}
}

func (h *HelloAO) Translations(context.Context) ([]Translation, error) {
	return []Translation{{ID: "kjv", Name: "King James Version", Language: "English", License: "Public domain via HelloAO", CanImport: true}}, nil
}

func (h *HelloAO) Lookup(ctx context.Context, q Query) (Passage, error) {
	if strings.ToLower(strings.TrimSpace(q.Translation)) != "kjv" {
		return Passage{}, invalid("translation", "Choose a supported translation.")
	}
	id, displayReference, err := passageID(q.Reference)
	if err != nil {
		return Passage{}, err
	}
	parts := strings.Split(id, ".")
	if len(parts) < 2 {
		return Passage{}, invalid("reference", "Use a book, chapter and optional verse range.")
	}
	endpoint := helloAOBaseURL + "/eng_kjv/" + parts[0] + "/" + parts[1] + ".json"
	var response helloAOChapter
	if err := h.getJSON(ctx, endpoint, &response); err != nil {
		return Passage{}, err
	}
	start, end := 1, response.NumberOfVerses
	if len(parts) == 3 {
		rangeParts := strings.Split(parts[2], "-")
		start = atoi(rangeParts[0])
		end = start
		if len(rangeParts) == 2 {
			end = atoi(rangeParts[1])
		}
	}
	if start < 1 || end < start || end > response.NumberOfVerses {
		return Passage{}, invalid("reference", "Reference not found. Use a valid chapter or verse range.")
	}
	verses := make([]Verse, 0, end-start+1)
	for _, item := range response.Chapter.Content {
		if item.Type != "verse" || item.Number < start || item.Number > end {
			continue
		}
		text := strings.TrimSpace(flattenHelloAOContent(item.Content))
		if text == "" || !plain(text, 30000) {
			return Passage{}, ErrUnavailable
		}
		verses = append(verses, Verse{BookID: parts[0], BookName: bookNames[parts[0]], Chapter: response.Chapter.Number, Number: item.Number, Text: text})
	}
	if len(verses) == 0 {
		return Passage{}, ErrUnavailable
	}
	texts := make([]string, 0, len(verses))
	for _, verse := range verses {
		texts = append(texts, "["+itoa(verse.Number)+"] "+verse.Text)
	}
	return Passage{Reference: displayReference, Translation: Translation{ID: "kjv", Name: "King James Version", Language: "English", License: "Public domain via HelloAO", CanImport: true}, Text: strings.Join(texts, "\n"), Verses: verses, Source: "HelloAO", SourceURL: "https://bible.helloao.org/"}, nil
}

type helloAOChapter struct {
	NumberOfVerses int `json:"numberOfVerses"`
	Chapter        struct {
		Number  int              `json:"number"`
		Content []helloAOContent `json:"content"`
	} `json:"chapter"`
}

type helloAOContent struct {
	Type    string            `json:"type"`
	Number  int               `json:"number"`
	Content []json.RawMessage `json:"content"`
}

func flattenHelloAOContent(items []json.RawMessage) string {
	parts := make([]string, 0, len(items))
	for _, raw := range items {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			parts = append(parts, text)
			continue
		}
		var formatted struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &formatted) == nil && formatted.Text != "" {
			parts = append(parts, formatted.Text)
		}
	}
	return strings.Join(parts, " ")
}

func (h *HelloAO) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(body) > 65536 || !json.Valid(body) || json.Unmarshal(body, target) != nil {
		return ErrUnavailable
	}
	return nil
}

func atoi(value string) int {
	result := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		result = result*10 + int(r-'0')
	}
	return result
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}

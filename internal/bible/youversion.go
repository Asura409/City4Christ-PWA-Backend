package bible

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const youVersionBaseURL = "https://api.youversion.com/v1"

// YouVersion is the server-side adapter for the YouVersion Platform API.
// The App Key is deliberately kept on the backend.
type YouVersion struct {
	client *http.Client
	key    string
	remote map[string]remoteYouVersionBible
}

type remoteYouVersionBible struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Abbreviation string `json:"abbreviation"`
	LanguageTag  string `json:"language_tag"`
	Copyright    string `json:"copyright"`
}

func NewYouVersion(key string, client *http.Client) *YouVersion {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	if c.Timeout <= 0 || c.Timeout > 5*time.Second {
		c.Timeout = 5 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &YouVersion{client: &c, key: strings.TrimSpace(key)}
}

func (y *YouVersion) Translations(ctx context.Context) ([]Translation, error) {
	if y.key == "" {
		return nil, ErrUnavailable
	}
	if err := y.loadCatalog(ctx); err != nil {
		return nil, err
	}
	result := make([]Translation, 0, len(y.remote))
	for id, item := range y.remote {
		license := "YouVersion"
		if strings.TrimSpace(item.Copyright) == "" {
			license = "Open access via YouVersion"
		}
		result = append(result, Translation{ID: id, Name: item.Title, Language: item.LanguageTag, License: license, CanImport: strings.TrimSpace(item.Copyright) == ""})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (y *YouVersion) loadCatalog(ctx context.Context) error {
	if y.remote != nil {
		return nil
	}
	query := url.Values{}
	query.Add("language_ranges[]", "en")
	query.Set("page_size", "99")
	var response struct {
		Data []remoteYouVersionBible `json:"data"`
	}
	if err := y.getJSON(ctx, youVersionBaseURL+"/bibles?"+query.Encode(), &response); err != nil {
		return err
	}
	remote := map[string]remoteYouVersionBible{}
	for _, item := range response.Data {
		if item.ID > 0 && strings.TrimSpace(item.Title) != "" {
			remote[strconv.Itoa(item.ID)] = item
		}
	}
	if len(remote) == 0 {
		return ErrUnavailable
	}
	y.remote = remote
	return nil
}

func (y *YouVersion) Lookup(ctx context.Context, q Query) (Passage, error) {
	if y.key == "" {
		return Passage{}, ErrUnavailable
	}
	q.Translation = strings.ToLower(strings.TrimSpace(q.Translation))
	if q.Translation == "" {
		return Passage{}, invalid("translation", "Choose a supported translation.")
	}
	if err := y.loadCatalog(ctx); err != nil {
		return Passage{}, err
	}
	remote, ok := y.remote[q.Translation]
	if !ok {
		return Passage{}, invalid("translation", "Choose a supported translation.")
	}
	passage, displayReference, err := passageID(q.Reference)
	if err != nil {
		return Passage{}, err
	}
	endpoint := fmt.Sprintf("%s/bibles/%d/passages/%s", youVersionBaseURL, remote.ID, url.PathEscape(passage))
	// The text response is compact, but it does not retain verse boundaries.
	// HTML responses include YouVersion's verse spans, which we convert to the
	// provider-neutral Verse model before returning plain text to the client.
	query := url.Values{"format": []string{"html"}, "include_headings": []string{"false"}, "include_notes": []string{"false"}}
	var response struct {
		ID        string `json:"id"`
		Reference string `json:"reference"`
		Content   string `json:"content"`
	}
	htmlErr := y.getJSON(ctx, endpoint+"?"+query.Encode(), &response)
	verses := parseYouVersionHTML(response.Content, passage)
	text := ""
	if htmlErr == nil && len(verses) > 0 {
		parts := make([]string, 0, len(verses))
		for _, verse := range verses {
			parts = append(parts, fmt.Sprintf("[%d] %s", verse.Number, verse.Text))
		}
		text = strings.Join(parts, " ")
	} else {
		// Keep the integration compatible with versions whose HTML response does
		// not expose the standard verse spans, or providers that do not support
		// the HTML representation at all. Those passages remain usable, but
		// cannot receive inline markers until the provider returns boundaries.
		textQuery := url.Values{"format": []string{"text"}}
		var textResponse struct {
			Content string `json:"content"`
		}
		if err := y.getJSON(ctx, endpoint+"?"+textQuery.Encode(), &textResponse); err != nil || !plain(textResponse.Content, 30000) {
			return Passage{}, ErrUnavailable
		}
		text = strings.TrimSpace(textResponse.Content)
	}
	translation := Translation{ID: q.Translation, Name: remote.Title, Language: remote.LanguageTag, License: "YouVersion", CanImport: strings.TrimSpace(remote.Copyright) == ""}
	if strings.TrimSpace(remote.Copyright) == "" {
		translation.License = "Open access via YouVersion"
	}
	return Passage{
		Reference:   displayReference,
		Translation: translation,
		Text:        text,
		Verses:      verses,
		Source:      "YouVersion",
		SourceURL:   "https://www.youversion.com/",
	}, nil
}

var youVersionSpanPattern = regexp.MustCompile(`(?is)<span\b([^>]*)>(.*?)</span>`)
var youVersionVerseNumberPattern = regexp.MustCompile(`(?i)\bv\s*=\s*["']([0-9]+)["']`)

// parseYouVersionHTML extracts the verse spans from YouVersion's HTML format.
// It deliberately strips markup so imported Daily verse text remains plain
// text and is safe to render with React's normal text escaping.
func parseYouVersionHTML(raw, passage string) []Verse {
	book := strings.Split(passage, ".")[0]
	matches := youVersionSpanPattern.FindAllStringSubmatch(raw, -1)
	result := make([]Verse, 0, len(matches))
	for _, match := range matches {
		attrs := match[1]
		if !strings.Contains(strings.ToLower(attrs), "yv-v") {
			continue
		}
		numberMatch := youVersionVerseNumberPattern.FindStringSubmatch(attrs)
		if len(numberMatch) != 2 {
			continue
		}
		number, err := strconv.Atoi(numberMatch[1])
		if err != nil {
			continue
		}
		text := strings.TrimSpace(html.UnescapeString(stripHTMLTags(match[2])))
		if text == "" {
			continue
		}
		result = append(result, Verse{BookID: book, BookName: bookNames[book], Number: number, Text: text})
	}
	return result
}

var htmlTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)

func stripHTMLTags(value string) string {
	return strings.Join(strings.Fields(htmlTagPattern.ReplaceAllString(value, " ")), " ")
}

func (y *YouVersion) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-YVP-App-Key", y.key)
	response, err := y.client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 || !json.Valid(body) {
		return ErrUnavailable
	}
	if err := json.Unmarshal(body, target); err != nil {
		return ErrUnavailable
	}
	return nil
}

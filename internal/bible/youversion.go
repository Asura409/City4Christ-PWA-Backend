package bible

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		result = append(result, Translation{ID: id, Name: item.Title, Language: item.LanguageTag, License: license})
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
	query := url.Values{"format": []string{"text"}}
	var response struct {
		ID        string `json:"id"`
		Reference string `json:"reference"`
		Content   string `json:"content"`
	}
	if err := y.getJSON(ctx, endpoint+"?"+query.Encode(), &response); err != nil {
		return Passage{}, err
	}
	if !plain(response.Content, 30000) {
		return Passage{}, ErrUnavailable
	}
	translation := Translation{ID: q.Translation, Name: remote.Title, Language: remote.LanguageTag, License: "YouVersion"}
	if strings.TrimSpace(remote.Copyright) == "" {
		translation.License = "Open access via YouVersion"
	}
	return Passage{
		Reference:   displayReference,
		Translation: translation,
		Text:        strings.TrimSpace(response.Content),
		Verses:      []Verse{{BookID: strings.Split(passage, ".")[0], BookName: bookNames[strings.Split(passage, ".")[0]], Text: strings.TrimSpace(response.Content)}},
		Source:      "YouVersion",
		SourceURL:   "https://www.youversion.com/",
	}, nil
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

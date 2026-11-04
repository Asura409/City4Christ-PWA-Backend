package bible

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiBibleBaseURL = "https://rest.api.bible/v1"

// APIBible is the server-side adapter for API.Bible. The API key is never
// exposed to the browser; the frontend continues to use our public Bible API.
type APIBible struct {
	client *http.Client
	key    string

	mu     sync.RWMutex
	remote map[string]string
	ready  bool
}

func NewAPIBible(key string, client *http.Client) *APIBible {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	if c.Timeout <= 0 || c.Timeout > 5*time.Second {
		c.Timeout = 5 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &APIBible{client: &c, key: strings.TrimSpace(key)}
}

type remoteBible struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	NameLocal         string `json:"nameLocal"`
	Abbreviation      string `json:"abbreviation"`
	AbbreviationLocal string `json:"abbreviationLocal"`
	Language          struct {
		ID string `json:"id"`
	} `json:"language"`
}

type remoteList struct {
	Data []remoteBible `json:"data"`
}

func (a *APIBible) Translations(ctx context.Context) ([]Translation, error) {
	if a.key == "" {
		return nil, ErrUnavailable
	}
	if err := a.loadCatalog(ctx); err != nil {
		return nil, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]Translation, 0, len(licensedCatalog))
	for _, t := range licensedCatalog {
		if _, ok := a.remote[t.ID]; ok {
			result = append(result, t)
		}
	}
	if len(result) == 0 {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (a *APIBible) loadCatalog(ctx context.Context) error {
	a.mu.RLock()
	ready := a.ready
	a.mu.RUnlock()
	if ready {
		return nil
	}
	u := apiBibleBaseURL + "/bibles?language=eng"
	var response remoteList
	if err := a.getJSON(ctx, u, &response); err != nil {
		return err
	}
	remote := map[string]string{}
	for _, item := range response.Data {
		abbreviation := strings.ToUpper(strings.TrimSpace(item.Abbreviation))
		localAbbreviation := strings.ToUpper(strings.TrimSpace(item.AbbreviationLocal))
		name := strings.ToUpper(strings.TrimSpace(item.Name + " " + item.NameLocal))
		switch {
		case abbreviation == "AMP" || localAbbreviation == "AMP" || strings.Contains(name, "AMPLIFIED BIBLE"):
			remote["amp"] = item.ID
		case abbreviation == "NKJV" || localAbbreviation == "NKJV" || strings.Contains(name, "NEW KING JAMES VERSION"):
			remote["nkjv"] = item.ID
		case abbreviation == "NIV" || abbreviation == "NIV11" || localAbbreviation == "NIV" || strings.Contains(name, "NEW INTERNATIONAL VERSION"):
			remote["niv"] = item.ID
		case abbreviation == "KJV" || strings.HasSuffix(abbreviation, "KJV") || localAbbreviation == "KJV" || (strings.Contains(name, "KING JAMES") && !strings.Contains(name, "NEW KING JAMES")):
			remote["kjv"] = item.ID
		}
	}
	a.mu.Lock()
	a.remote = remote
	a.ready = true
	a.mu.Unlock()
	return nil
}

type remotePassage struct {
	ID         string          `json:"id"`
	Reference  string          `json:"reference"`
	Content    json.RawMessage `json:"content"`
	Copyright  string          `json:"copyright"`
	VerseCount int             `json:"verseCount"`
}

type apiResponse[T any] struct {
	Data T `json:"data"`
}

type contentNode struct {
	Name  string                     `json:"name"`
	Type  string                     `json:"type"`
	Text  string                     `json:"text"`
	Attrs map[string]json.RawMessage `json:"attrs"`
	Items []contentNode              `json:"items"`
}

func contentAttr(node contentNode, name string) string {
	var value string
	if json.Unmarshal(node.Attrs[name], &value) != nil {
		return ""
	}
	return value
}

var referencePattern = regexp.MustCompile(`^(.+?)\s+(\d+)(?::(\d+)(?:-(?:(\d+):)?(\d+))?)?$`)

var bookIDs = map[string]string{
	"genesis": "GEN", "exodus": "EXO", "leviticus": "LEV", "numbers": "NUM", "deuteronomy": "DEU",
	"joshua": "JOS", "judges": "JDG", "ruth": "RUT", "1 samuel": "1SA", "2 samuel": "2SA", "1 kings": "1KI", "2 kings": "2KI",
	"1 chronicles": "1CH", "2 chronicles": "2CH", "ezra": "EZR", "nehemiah": "NEH", "esther": "EST", "job": "JOB", "psalms": "PSA", "psalm": "PSA",
	"proverbs": "PRO", "ecclesiastes": "ECC", "song of solomon": "SNG", "song of songs": "SNG", "isaiah": "ISA", "jeremiah": "JER", "lamentations": "LAM",
	"ezekiel": "EZK", "daniel": "DAN", "hosea": "HOS", "joel": "JOL", "amos": "AMO", "obadiah": "OBA", "jonah": "JON", "micah": "MIC",
	"nahum": "NAM", "habakkuk": "HAB", "zephaniah": "ZEP", "haggai": "HAG", "zechariah": "ZEC", "malachi": "MAL", "matthew": "MAT", "mark": "MRK",
	"luke": "LUK", "john": "JHN", "acts": "ACT", "romans": "ROM", "1 corinthians": "1CO", "2 corinthians": "2CO", "galatians": "GAL",
	"ephesians": "EPH", "philippians": "PHP", "colossians": "COL", "1 thessalonians": "1TH", "2 thessalonians": "2TH", "1 timothy": "1TI",
	"2 timothy": "2TI", "titus": "TIT", "philemon": "PHM", "hebrews": "HEB", "james": "JAS", "1 peter": "1PE", "2 peter": "2PE",
	"1 john": "1JN", "2 john": "2JN", "3 john": "3JN", "jude": "JUD", "revelation": "REV",
}

var bookNames = map[string]string{
	"GEN": "Genesis", "EXO": "Exodus", "LEV": "Leviticus", "NUM": "Numbers", "DEU": "Deuteronomy", "JOS": "Joshua", "JDG": "Judges", "RUT": "Ruth",
	"1SA": "1 Samuel", "2SA": "2 Samuel", "1KI": "1 Kings", "2KI": "2 Kings", "1CH": "1 Chronicles", "2CH": "2 Chronicles", "EZR": "Ezra", "NEH": "Nehemiah", "EST": "Esther", "JOB": "Job", "PSA": "Psalms", "PRO": "Proverbs", "ECC": "Ecclesiastes", "SNG": "Song of Solomon", "ISA": "Isaiah", "JER": "Jeremiah", "LAM": "Lamentations", "EZK": "Ezekiel", "DAN": "Daniel", "HOS": "Hosea", "JOL": "Joel", "AMO": "Amos", "OBA": "Obadiah", "JON": "Jonah", "MIC": "Micah", "NAM": "Nahum", "HAB": "Habakkuk", "ZEP": "Zephaniah", "HAG": "Haggai", "ZEC": "Zechariah", "MAL": "Malachi", "MAT": "Matthew", "MRK": "Mark", "LUK": "Luke", "JHN": "John", "ACT": "Acts", "ROM": "Romans", "1CO": "1 Corinthians", "2CO": "2 Corinthians", "GAL": "Galatians", "EPH": "Ephesians", "PHP": "Philippians", "COL": "Colossians", "1TH": "1 Thessalonians", "2TH": "2 Thessalonians", "1TI": "1 Timothy", "2TI": "2 Timothy", "TIT": "Titus", "PHM": "Philemon", "HEB": "Hebrews", "JAS": "James", "1PE": "1 Peter", "2PE": "2 Peter", "1JN": "1 John", "2JN": "2 John", "3JN": "3 John", "JUD": "Jude", "REV": "Revelation",
}

func passageID(reference string) (string, string, error) {
	m := referencePattern.FindStringSubmatch(strings.TrimSpace(reference))
	if m == nil {
		return "", "", invalid("reference", "Use a book, chapter and optional verse range.")
	}
	book, ok := bookIDs[strings.ToLower(strings.TrimSpace(m[1]))]
	if !ok {
		return "", "", invalid("reference", "Use a recognized English Bible book name.")
	}
	chapter := m[2]
	if m[3] == "" {
		return book + "." + chapter, bookNames[book] + " " + chapter, nil
	}
	start := book + "." + chapter + "." + m[3]
	if m[5] == "" {
		return start, bookNames[book] + " " + chapter + ":" + m[3], nil
	}
	endChapter := chapter
	if m[4] != "" {
		endChapter = m[4]
	}
	end := book + "." + endChapter + "." + m[5]
	return start + "-" + end, bookNames[book] + " " + chapter + ":" + m[3] + "-" + m[5], nil
}

func (a *APIBible) Lookup(ctx context.Context, q Query) (Passage, error) {
	q, err := normalizeWithCatalog(q, true)
	if err != nil {
		return Passage{}, err
	}
	if a.key == "" {
		return Passage{}, ErrUnavailable
	}
	if err := a.loadCatalog(ctx); err != nil {
		return Passage{}, err
	}
	a.mu.RLock()
	remoteID := a.remote[q.Translation]
	a.mu.RUnlock()
	if remoteID == "" {
		return Passage{}, invalid("translation", "That Bible version is not enabled for this API key.")
	}
	passage, displayReference, err := passageID(q.Reference)
	if err != nil {
		return Passage{}, err
	}
	// A chapter ID has two dots; a verse or range has three or more.
	var endpoint string
	if strings.Count(passage, ".") == 1 {
		endpoint = fmt.Sprintf("%s/bibles/%s/chapters/%s", apiBibleBaseURL, url.PathEscape(remoteID), url.PathEscape(passage))
	} else {
		endpoint = fmt.Sprintf("%s/bibles/%s/passages/%s", apiBibleBaseURL, url.PathEscape(remoteID), url.PathEscape(passage))
	}
	query := url.Values{"content-type": []string{"json"}, "include-titles": []string{"false"}, "include-verse-numbers": []string{"true"}, "include-notes": []string{"false"}}
	var response apiResponse[remotePassage]
	if err := a.getJSON(ctx, endpoint+"?"+query.Encode(), &response); err != nil {
		return Passage{}, err
	}
	translation, _ := findAnyTranslation(q.Translation)
	if response.Data.Copyright != "" {
		translation.License = response.Data.Copyright
	}
	verses := parseContent(response.Data.Content)
	if len(verses) == 0 {
		return Passage{}, ErrUnavailable
	}
	text := make([]string, 0, len(verses))
	for _, verse := range verses {
		text = append(text, verse.Text)
	}
	if response.Data.Reference != "" {
		displayReference = response.Data.Reference
	}
	return Passage{Reference: displayReference, Translation: translation, Text: strings.Join(text, "\n"), Verses: verses, Source: "API.Bible", SourceURL: "https://api.bible/"}, nil
}

func parseContent(raw json.RawMessage) []Verse {
	var nodes []contentNode
	if json.Unmarshal(raw, &nodes) != nil {
		return nil
	}
	var result []Verse
	index := map[string]int{}
	var walk func([]contentNode, string)
	walk = func(items []contentNode, current string) {
		for _, node := range items {
			if node.Name == "verse" && contentAttr(node, "sid") != "" {
				current = contentAttr(node, "sid")
			}
			if verseID := contentAttr(node, "verseId"); verseID != "" && strings.TrimSpace(node.Text) != "" {
				parts := strings.FieldsFunc(verseID, func(r rune) bool { return r == '.' })
				if len(parts) == 3 {
					key := verseID
					if _, ok := index[key]; !ok {
						chapter, _ := strconv.Atoi(parts[1])
						number, _ := strconv.Atoi(parts[2])
						index[key] = len(result)
						result = append(result, Verse{BookID: parts[0], BookName: bookNames[parts[0]], Chapter: chapter, Number: number})
					}
					result[index[key]].Text += strings.TrimSpace(node.Text) + " "
				}
			}
			walk(node.Items, current)
		}
	}
	walk(nodes, "")
	for i := range result {
		result[i].Text = strings.TrimSpace(result[i].Text)
	}
	return result
}

func (a *APIBible) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("api-key", a.key)
	response, err := a.client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusNotFound:
		return invalid("reference", "Reference not found. Use a book, chapter and optional verse range.")
	case http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || len(body) > 2<<20 || json.Unmarshal(body, target) != nil {
		return ErrUnavailable
	}
	return nil
}

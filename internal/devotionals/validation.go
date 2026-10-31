package devotionals

import (
	"encoding/base64"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func validID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}
func normalize(c Content) (Content, error) {
	for _, f := range []struct {
		value     *string
		name      string
		min, max  int
		multiline bool
	}{{&c.Title, "title", 1, 200, false}, {&c.Body, "body", 1, 20000, true}, {&c.ScriptureReference, "scriptureReference", 0, 200, false}, {&c.Author, "author", 0, 200, false}} {
		*f.value = strings.TrimSpace(*f.value)
		n := utf8.RuneCountInString(*f.value)
		if !utf8.ValidString(*f.value) || n < f.min || n > f.max {
			return Content{}, invalid(f.name, "Use text within the documented length.")
		}
		for _, r := range *f.value {
			if unicode.IsControl(r) && !(f.multiline && (r == '\n' || r == '\r' || r == '\t')) {
				return Content{}, invalid(f.name, "Remove unsupported control characters.")
			}
		}
	}
	return c, nil
}
func decodeCursor(raw string) (any, any, error) {
	if raw == "" {
		return nil, nil, nil
	}
	if len(raw) > 200 {
		return nil, nil, invalid("cursor", "Use a cursor returned by this endpoint.")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	parts := strings.Split(string(b), "|")
	if err != nil || len(parts) != 2 || !validID(parts[1]) {
		return nil, nil, invalid("cursor", "Use a cursor returned by this endpoint.")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || at.Year() < 1 || at.Year() > 9999 {
		return nil, nil, invalid("cursor", "Use a cursor returned by this endpoint.")
	}
	return at, parts[1], nil
}
func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}
func options(o Options) (Options, error) {
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 {
		return o, invalid("limit", "Choose a limit from 1 to 100.")
	}
	_, _, err := decodeCursor(o.Cursor)
	return o, err
}

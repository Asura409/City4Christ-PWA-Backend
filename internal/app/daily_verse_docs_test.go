package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestDailyVerseOpenAPIContract(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths      map[string]map[string]map[string]json.RawMessage
		Components struct {
			Schemas map[string]struct{ Properties map[string]json.RawMessage }
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		path, method string
		private      bool
	}{{"/api/v1/daily-verse", "get", false}, {"/api/v1/admin/daily-verses", "get", true}, {"/api/v1/admin/daily-verses", "post", true}, {"/api/v1/admin/daily-verses/{verseId}", "get", true}, {"/api/v1/admin/daily-verses/{verseId}", "patch", true}} {
		op := spec.Paths[r.path][r.method]
		if len(op) == 0 {
			t.Fatalf("missing daily verse operation %s %s", r.method, r.path)
		}
		var security []map[string][]string
		if err := json.Unmarshal(op["security"], &security); err != nil || r.private != (len(security) > 0) {
			t.Fatal("incorrect daily verse security", r.path, err)
		}
	}
	today := spec.Components.Schemas["TodayDailyVerse"].Properties
	for _, f := range []string{"date", "timeZone", "serverTime", "nextDayAt", "verse"} {
		if len(today[f]) == 0 {
			t.Fatal("missing clock/empty-state contract", f)
		}
	}
	public := spec.Components.Schemas["PublishedDailyVerse"].Properties
	for _, f := range []string{"version", "status", "actorId", "createdAt"} {
		if _, ok := public[f]; ok {
			t.Fatal("editorial metadata leaked", f)
		}
	}
}

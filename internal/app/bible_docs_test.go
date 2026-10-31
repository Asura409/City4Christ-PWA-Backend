package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestBibleOpenAPIContract(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var s struct {
		Paths map[string]map[string]struct {
			Security  []any          `json:"security"`
			Responses map[string]any `json:"responses"`
		} `json:"paths"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &s); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"/api/v1/bible/translations", "/api/v1/bible/passages"} {
		op, ok := s.Paths[p]["get"]
		if !ok {
			t.Fatal("missing Bible operation", p)
		}
		if len(op.Security) != 0 || op.Responses["200"] == nil || op.Responses["429"] == nil || op.Responses["503"] == nil {
			t.Fatal("incomplete public Bible contract", p)
		}
	}
}

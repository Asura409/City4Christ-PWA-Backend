package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestVisitorOpenAPIContract(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var s struct {
		Paths      map[string]map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &s); e != nil {
		t.Fatal(e)
	}
	for path, method := range map[string]string{"/api/v1/visitors/config": "get", "/api/v1/visitors": "post", "/api/v1/admin/visitors": "get", "/api/v1/admin/visitors/{visitorId}": "patch"} {
		if len(s.Paths[path][method]) == 0 {
			t.Fatal("missing visitor operation", path)
		}
	}
	for _, field := range []string{"email", "phone", "sealedPayload", "submissionId", "keyId"} {
		if len(s.Components.Schemas["VisitorSummary"].Properties[field]) > 0 {
			t.Fatal("list contract leaks private fields", field)
		}
	}
}

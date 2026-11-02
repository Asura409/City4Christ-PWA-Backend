package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAccountDeletionOpenAPIContract(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]struct {
			Security  []map[string][]string      `json:"security"`
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties           map[string]json.RawMessage `json:"properties"`
				AdditionalProperties *bool                      `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &spec); e != nil {
		t.Fatal(e)
	}
	for path, methods := range map[string][]string{"/api/v1/me/deletion-request": {"get"}, "/api/v1/me": {"delete"}, "/api/v1/admin/account-deletion-requests": {"get"}, "/api/v1/admin/account-deletion-requests/{requestId}": {"get", "patch"}} {
		for _, method := range methods {
			op, ok := spec.Paths[path][method]
			if !ok {
				t.Fatalf("missing account deletion operation %s %s", method, path)
			}
			if len(op.Security) != 1 {
				t.Fatal("missing Bearer authentication", path)
			}
			if _, ok := op.Security[0]["bearerAuth"]; !ok {
				t.Fatal("missing Bearer security", path)
			}
			for _, code := range []string{"401", "403", "422", "429", "503"} {
				if len(op.Responses[code]) == 0 {
					t.Fatalf("missing deletion error %s %s", path, code)
				}
			}
		}
	}
	if len(spec.Paths["/api/v1/me"]["delete"].Responses["202"]) == 0 {
		t.Fatal("deletion must document accepted review request, not completed purge")
	}
	for _, name := range []string{"AccountDeletionInput", "AccountDeletionReviewInput"} {
		s, ok := spec.Components.Schemas[name]
		if !ok || s.AdditionalProperties == nil || *s.AdditionalProperties {
			t.Fatal("missing strict deletion input", name)
		}
	}
	for _, field := range []string{"email", "phone", "passwordHash"} {
		if len(spec.Components.Schemas["AccountDeletionSummary"].Properties[field]) > 0 {
			t.Fatal("deletion queue leaks private field", field)
		}
	}
	if len(spec.Components.Schemas["AccountDeletionDetail"].Properties["email"]) == 0 {
		t.Fatal("missing explicit audited detail email")
	}
}

package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestPastoralContractDocumentsPrivateApprovalFlow(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths      map[string]map[string]json.RawMessage
		Components struct {
			Schemas map[string]struct{ Properties map[string]json.RawMessage }
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ path, method string }{{"/api/v1/pastors", "get"}, {"/api/v1/admin/pastors", "post"}, {"/api/v1/pastors/{pastorId}/availability", "get"}, {"/api/v1/me/pastoral/profile", "get"}, {"/api/v1/me/pastoral/availability", "get"}, {"/api/v1/me/pastoral/availability", "post"}, {"/api/v1/me/pastoral/availability/{slotId}", "delete"}, {"/api/v1/pastor-bookings", "post"}, {"/api/v1/me/pastor-bookings", "get"}, {"/api/v1/pastor-bookings/{bookingId}", "delete"}, {"/api/v1/me/pastoral/bookings", "get"}, {"/api/v1/me/pastoral/bookings/{bookingId}", "patch"}} {
		if len(spec.Paths[item.path][item.method]) == 0 {
			t.Fatalf("missing pastoral contract %s %s", item.method, item.path)
		}
	}
	profile := spec.Components.Schemas["PastorProfile"].Properties
	if _, ok := profile["email"]; ok {
		t.Fatal("pastor contact details exposed")
	}
	if len(spec.Components.Schemas["PastoralBooking"].Properties) == 0 {
		t.Fatal("missing booking schema")
	}
}

func TestEveryDocumentedOperationUsesDeclaredSecurity(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths      map[string]map[string]struct{ Security []map[string][]string }
		Components struct{ SecuritySchemes map[string]json.RawMessage }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for path, methods := range spec.Paths {
		for method, op := range methods {
			for _, requirements := range op.Security {
				for scheme := range requirements {
					if _, ok := spec.Components.SecuritySchemes[scheme]; !ok {
						t.Fatalf("undeclared security scheme %s in %s %s", scheme, method, path)
					}
				}
			}
		}
	}
}

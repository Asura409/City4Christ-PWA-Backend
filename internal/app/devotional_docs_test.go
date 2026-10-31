package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestDevotionalOpenAPIContract(t *testing.T) {
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
	for _, route := range []struct {
		path, method string
		private      bool
	}{{"/api/v1/devotionals", "get", false}, {"/api/v1/devotionals/{devotionalId}", "get", false}, {"/api/v1/admin/devotionals", "get", true}, {"/api/v1/admin/devotionals", "post", true}, {"/api/v1/admin/devotionals/{devotionalId}", "get", true}, {"/api/v1/admin/devotionals/{devotionalId}", "patch", true}} {
		op := spec.Paths[route.path][route.method]
		if len(op) == 0 {
			t.Fatalf("missing devotional operation %s %s", route.method, route.path)
		}
		var security []map[string][]string
		if err := json.Unmarshal(op["security"], &security); err != nil {
			t.Fatal("security must be explicit", err)
		}
		if route.private != (len(security) > 0) {
			t.Fatal("incorrect publication security", route.path)
		}
	}
	var permission struct{ Enum []string }
	if err := json.Unmarshal(spec.Components.Schemas["CurrentCapability"].Properties["permission"], &permission); err != nil {
		t.Fatal(err)
	}
	declared := false
	for _, p := range permission.Enum {
		if p == "devotionals.manage" {
			declared = true
		}
	}
	if !declared {
		t.Fatal("current capability schema omits devotional authority")
	}
	fields := spec.Components.Schemas["PublishedDevotional"].Properties
	if len(fields) == 0 {
		t.Fatal("public devotional schema missing")
	}
	for _, f := range []string{"version", "createdBy", "actorId", "status"} {
		if _, ok := fields[f]; ok {
			t.Fatal("editorial data exposed", f)
		}
	}
}

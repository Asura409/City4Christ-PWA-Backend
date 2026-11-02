package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminMemberSearchThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, staff := f.account(t, "staff", false)
	_, member := f.account(t, "", false)
	if w := f.request("GET", "/api/v1/admin/members?query=Church&limit=1", "", ""); w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
	if w := f.request("GET", "/api/v1/admin/members?query=Church", member, ""); w.Code != 403 {
		t.Fatalf("member %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?query=Ch", "?query=Church&cursor=no", "?query=Church&limit=99", "?query=Church&roles=true"} {
		if w := f.request("GET", "/api/v1/admin/members"+suffix, staff, ""); w.Code != 422 {
			t.Fatalf("invalid %s: %d %s", suffix, w.Code, w.Body.String())
		}
	}
	w := f.request("GET", "/api/v1/admin/members?query=Church&limit=1", staff, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("search %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data []map[string]json.RawMessage `json:"data"`
		Meta struct {
			NextCursor string `json:"nextCursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || len(payload.Data) != 1 || payload.Meta.NextCursor == "" {
		t.Fatalf("pagination %s %v", w.Body.String(), err)
	}
	for key := range payload.Data[0] {
		if key != "id" && key != "firstName" && key != "lastName" && key != "email" && key != "joinedAt" {
			t.Fatalf("private field %s", key)
		}
	}
	if strings.Contains(w.Body.String(), "mfa") || strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "role") {
		t.Fatalf("security fields leaked: %s", w.Body.String())
	}
}

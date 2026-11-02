package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestGroupNotificationOpenAPIContract(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var s struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	for path, methods := range map[string][]string{"/api/v1/notifications/preferences": {"get", "put"}, "/api/v1/notifications/read-all": {"put"}, "/api/v1/notifications/push/config": {"get"}, "/api/v1/notifications/push/subscriptions": {"post", "delete"}, "/api/v1/admin/broadcasts": {"get", "post"}, "/api/v1/admin/broadcasts/{broadcastId}": {"get"}, "/api/v1/admin/broadcasts/{broadcastId}/send": {"post"}} {
		for _, m := range methods {
			if len(s.Paths[path][m]) == 0 {
				t.Fatal("undocumented operation", m, path)
			}
		}
	}
	for _, name := range []string{"ChurchPost", "ChurchEvent"} {
		if len(s.Components.Schemas[name].Properties["groupId"]) == 0 {
			t.Fatal("undocumented group audience", name)
		}
	}
	var listBroadcasts struct {
		Parameters []struct {
			Name string `json:"name"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(s.Paths["/api/v1/admin/broadcasts"]["get"], &listBroadcasts); err != nil {
		t.Fatal(err)
	}
	hasAudience := false
	for _, parameter := range listBroadcasts.Parameters {
		hasAudience = hasAudience || parameter.Name == "audience"
	}
	if !hasAudience {
		t.Fatal("broadcast list omits audience query")
	}
	var broadcastAudience struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(s.Components.Schemas["BroadcastInput"].Properties["audience"], &broadcastAudience); err != nil {
		t.Fatal(err)
	}
	hasStaff := false
	for _, value := range broadcastAudience.Enum {
		hasStaff = hasStaff || value == "staff"
	}
	if !hasStaff {
		t.Fatal("broadcast input omits staff audience")
	}
}

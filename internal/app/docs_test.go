package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationThroughGlobalApp(t *testing.T) {
	handler := New(nil, Config{})
	for _, path := range []string{"/docs/", "/docs/swagger-ui.css", "/docs/swagger-ui-bundle.js", "/docs/init.js", "/openapi.json"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if path == "/openapi.json" {
			var spec struct {
				OpenAPI string                     `json:"openapi"`
				Paths   map[string]json.RawMessage `json:"paths"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/api/v1/members", "/api/v1/admin/accounts/{accountId}/roles", "/api/v1/admin/accounts/{accountId}/roles/revoke", "/api/v1/admin/sermons", "/api/v1/admin/sermons/{sermonId}", "/api/v1/sermons", "/api/v1/sermons/{sermonId}", "/api/v1/groups", "/api/v1/admin/groups", "/api/v1/admin/groups/{groupId}", "/api/v1/admin/groups/{groupId}/leaders", "/api/v1/groups/{groupId}/membership", "/api/v1/groups/{groupId}/members", "/api/v1/groups/{groupId}/messages", "/api/v1/groups/{groupId}/messages/{messageId}", "/api/v1/groups/{groupId}/read", "/api/v1/group-conversations", "/api/v1/groups/{groupId}/membership-requests", "/api/v1/groups/{groupId}/membership-requests/{accountId}", "/api/v1/posts", "/api/v1/posts/{postId}/reaction", "/api/v1/events", "/api/v1/events/{eventId}", "/api/v1/events/{eventId}/calendar.ics", "/api/v1/events/{eventId}/coordinators", "/api/v1/events/{eventId}/coordinators/{accountId}", "/api/v1/prayer/requests", "/api/v1/prayer/requests/{requestId}", "/api/v1/prayer/requests/{requestId}/reports", "/api/v1/prayer/reports", "/api/v1/prayer/reports/{reportId}", "/api/v1/conversations", "/api/v1/conversations/{conversationId}/messages", "/api/v1/conversations/{conversationId}/read", "/api/v1/events/{eventId}/rsvp", "/api/v1/notifications", "/api/v1/notifications/unread-count", "/api/v1/notifications/{notificationId}/read", "/api/v1/admin/events", "/api/v1/admin/events/{eventId}", "/health/live", "/health/ready", "/api/v1/auth/register", "/api/v1/auth/verification/verify", "/api/v1/auth/verification/resend", "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/logout", "/api/v1/me", "/api/v1/auth/google/start", "/api/v1/auth/google/link", "/api/v1/auth/google/callback"} {
				if _, ok := spec.Paths[route]; !ok {
					t.Fatalf("missing contract: %s", route)
				}
			}
		}
		if path == "/docs/init.js" && (!strings.Contains(w.Body.String(), "withCredentials: true") || !strings.Contains(w.Body.String(), "validatorUrl: null")) {
			t.Fatal("missing same-origin cookie or private validator configuration")
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code != 308 || w.Header().Get("Location") != "/docs/" {
		t.Fatal("docs redirect missing")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/docs/not-found", nil))
	if w.Code != 404 {
		t.Fatal("unknown docs asset exposed")
	}
}

func TestProfileAndSermonRequestContract(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	if _, ok := paths["/api/v1/me"].(map[string]any)["patch"]; !ok {
		t.Fatal("profile PATCH contract missing")
	}
	for _, tc := range []struct{ path, method string }{{"/api/v1/admin/sermons", "post"}, {"/api/v1/admin/sermons/{sermonId}", "patch"}} {
		op := paths[tc.path].(map[string]any)[tc.method].(map[string]any)
		body := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
		examples, ok := body["examples"].(map[string]any)
		if !ok || len(examples) == 0 {
			t.Fatalf("usable examples missing: %s", tc.path)
		}
		for _, example := range examples {
			value := example.(map[string]any)["value"].(map[string]any)
			if value["videoUrl"] == "string" || value["id"] != nil || value["publishedAt"] != nil {
				t.Fatalf("response-only/placeholder values in request example: %+v", value)
			}
		}
	}
}

func TestDirectoryContractContainsOnlyApprovedFields(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	route, ok := paths["/api/v1/members"].(map[string]any)
	if !ok {
		t.Fatal("member directory contract missing")
	}
	get := route["get"].(map[string]any)
	if len(get["security"].([]any)) == 0 {
		t.Fatal("directory contract permits anonymous access")
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	member, ok := schemas["DirectoryMember"].(map[string]any)
	if !ok {
		t.Fatal("directory member DTO missing")
	}
	fields := member["properties"].(map[string]any)
	if len(fields) != 3 || fields["id"] == nil || fields["firstName"] == nil || fields["lastName"] == nil || member["additionalProperties"] != false {
		t.Fatal("directory schema exposes unapproved fields")
	}
}

func TestPasswordRecoveryContract(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	for _, path := range []string{"/api/v1/auth/password/forgot", "/api/v1/auth/password/reset"} {
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("missing recovery contract: %s", path)
		}
		op := route["post"].(map[string]any)
		if len(op["security"].([]any)) != 0 {
			t.Fatal("recovery requires signed-in credentials")
		}
	}
}

func TestCurrentAccessContract(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	route, ok := paths["/api/v1/me/access"].(map[string]any)
	if !ok {
		t.Fatal("current access contract missing")
	}
	if len(route) != 1 {
		t.Fatal("current access exposes mutation methods")
	}
	get := route["get"].(map[string]any)
	if len(get["security"].([]any)) == 0 {
		t.Fatal("access snapshot permits anonymous requests")
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	current := schemas["CurrentAccess"].(map[string]any)["properties"].(map[string]any)
	if len(current) != 2 || current["roles"] == nil || current["capabilities"] == nil {
		t.Fatal("access snapshot exposes unexpected fields")
	}
}

func TestGroupAudienceContractUsesChurchRoles(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"Group", "CreateGroupInput"} {
		fields := schemas[name].(map[string]any)["properties"].(map[string]any)
		if fields["audience"] == nil || fields["visibility"] != nil {
			t.Fatalf("%s must describe group audience, not private visibility: %+v", name, fields)
		}
		values := fields["audience"].(map[string]any)["enum"].([]any)
		if len(values) != 2 || values[0] != "church" || values[1] != "leadership" {
			t.Fatalf("unexpected group audiences: %v", values)
		}
	}
}

func TestReauthenticationContractRequiresBearerAndSeparateConfirmation(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	for _, tc := range []struct{ path, method string }{{"/api/v1/auth/reauthentication", "get"}, {"/api/v1/auth/reauthentication/password", "post"}, {"/api/v1/auth/reauthentication/google", "post"}} {
		entry, ok := paths[tc.path].(map[string]any)
		if !ok {
			t.Fatalf("missing reauthentication contract %s", tc.path)
		}
		operation := entry[tc.method].(map[string]any)
		if len(operation["security"].([]any)) != 1 {
			t.Fatal("missing Bearer requirement")
		}
		if _, ok := operation["responses"].(map[string]any)["429"]; !ok {
			t.Fatal("missing rate limit contract")
		}
	}
}

func TestMFAContractRequiresPrivateBearerRoutes(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	for _, suffix := range []string{"", "/enroll", "/confirm", "/verify", "/recovery", "/recovery/regenerate", "/disable"} {
		path := "/api/v1/auth/mfa" + suffix
		entry, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatal("missing MFA contract", path)
		}
		method := "post"
		if suffix == "" {
			method = "get"
		}
		op := entry[method].(map[string]any)
		if len(op["security"].([]any)) != 1 {
			t.Fatal("MFA endpoint missing Bearer security")
		}
	}
}

func TestChurchRoleContract(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, method string }{{"/api/v1/admin/role-candidates", "get"}, {"/api/v1/admin/accounts/{accountId}/church-roles", "get"}, {"/api/v1/admin/accounts/{accountId}/church-roles", "post"}, {"/api/v1/admin/accounts/{accountId}/church-roles/revoke", "post"}} {
		op := spec.Paths[tc.path][tc.method]
		if op == nil {
			t.Fatalf("missing church role contract: %s %s", tc.method, tc.path)
		}
		if len(op["security"].([]any)) == 0 {
			t.Fatal("anonymous protected role contract")
		}
	}
	candidate := spec.Components.Schemas["RoleCandidate"]
	if candidate == nil {
		t.Fatal("candidate contract missing")
	}
	fields := candidate["properties"].(map[string]any)
	if len(fields) != 5 || fields["mfaEnabled"] == nil || candidate["additionalProperties"] != false {
		t.Fatal("candidate privacy contract")
	}
}

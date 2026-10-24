package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/events"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestEventsHTTPHandlersEnforceAuthenticationOriginAndInput(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("e", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	var adminID string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('handler-admin@example.invalid','Event','Admin',now()) RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'super_administrator','')`, adminID); err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(context.Background(), adminID)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	service := events.NewService(events.NewRepository(db), nil)
	eventRoutes(mux, service, sessions, nil, Config{AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/v1/admin/events", "", `{}`, "https://api.example"); w.Code != 401 {
		t.Fatalf("anonymous event creation status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/admin/events", tokens.AccessToken, `{}`, "https://evil.example"); w.Code != 403 {
		t.Fatalf("event creation with disallowed origin status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/events?admin=true", "", "", ""); w.Code != 422 {
		t.Fatalf("unknown public query status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/events/11111111-1111-4111-8111-111111111111/rsvp", "", `{"attendeeCount":1}`, "https://api.example"); w.Code != 401 {
		t.Fatalf("anonymous RSVP status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/admin/events", tokens.AccessToken, `{"title":"Event","startsAt":"2026-11-02T10:00:00Z","timeZone":"Mars/Olympus"}`, "https://api.example"); w.Code != 422 {
		t.Fatalf("invalid timezone status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEventsThroughGlobalAppWithCapacityAndRoleBoundaries(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("v", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(email, role string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Events','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,'')`, id, role); err != nil {
				t.Fatal(err)
			}
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	_, adminToken := actor("event-admin@example.invalid", "super_administrator")
	_, staffToken := actor("event-staff@example.invalid", "staff")
	memberID, memberToken := actor("event-member@example.invalid", "")
	_, otherToken := actor("event-other@example.invalid", "")
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/v1/admin/events", memberToken, `{ "title":"Church Picnic" }`, "https://api.example"); w.Code != 403 {
		t.Fatalf("member created event: %d %s", w.Code, w.Body.String())
	}
	startsAt := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{"title": "Church Picnic", "description": "Community lunch", "location": "Church grounds", "startsAt": startsAt.Format(time.RFC3339), "timeZone": "Africa/Lagos", "capacity": 3})
	w := request("POST", "/api/v1/admin/events", staffToken, string(body), "https://api.example")
	if w.Code != 201 {
		t.Fatalf("staff create event: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data events.Event `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Data.ID == "" || created.Data.Status != "draft" {
		t.Fatalf("created response=%s err=%v", w.Body.String(), err)
	}
	id := created.Data.ID
	if w = request("GET", "/api/v1/events", "", "", ""); w.Code != 200 || strings.Contains(w.Body.String(), id) {
		t.Fatalf("draft leaked from public list: %d %s", w.Code, w.Body.String())
	}
	update, _ := json.Marshal(map[string]any{"title": "Church Picnic", "description": "Community lunch", "location": "Church grounds", "startsAt": startsAt.Format(time.RFC3339), "timeZone": "Africa/Lagos", "capacity": 3, "status": "published", "version": created.Data.Version})
	if w = request("PATCH", "/api/v1/admin/events/"+id, adminToken, string(update), "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"published"`) {
		t.Fatalf("admin publish: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/events/"+id, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Church Picnic") {
		t.Fatalf("public event detail: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/events/"+id+"/rsvp", memberToken, `{"attendeeCount":2}`, "https://api.example"); w.Code != 200 {
		t.Fatalf("member RSVP: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/events/"+id+"/rsvp", memberToken, `{"attendeeCount":2}`, "https://api.example"); w.Code != 200 {
		t.Fatalf("repeat RSVP should be idempotent: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/events/"+id+"/rsvp", otherToken, `{"attendeeCount":2}`, "https://api.example"); w.Code != 409 || !strings.Contains(w.Body.String(), "event_full") {
		t.Fatalf("capacity conflict: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/events/"+id, memberToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"attendeeCount":2`) || !strings.Contains(w.Body.String(), `"myAttendeeCount":2`) {
		t.Fatalf("event counts: %d %s", w.Code, w.Body.String())
	}
	if w = request("DELETE", "/api/v1/events/"+id+"/rsvp", memberToken, "", "https://api.example"); w.Code != 200 {
		t.Fatalf("cancel RSVP: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/events/"+id+"/rsvp", otherToken, `{"attendeeCount":3}`, "https://api.example"); w.Code != 200 {
		t.Fatalf("freed seats were not available: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/me/access", staffToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"permission":"events.manage"`) {
		t.Fatalf("events.manage missing from staff capabilities: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/events/"+id, memberToken, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), memberID) {
		t.Fatalf("public event leaked attendee identity: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/admin/events", otherToken, "", ""); w.Code != 403 {
		t.Fatalf("member read draft management listing: %d %s", w.Code, w.Body.String())
	}
}

func TestEventsOpenAPIContract(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/events", "/api/v1/events/{eventId}", "/api/v1/events/{eventId}/rsvp", "/api/v1/admin/events", "/api/v1/admin/events/{eventId}"} {
		if spec.Paths[path] == nil {
			t.Errorf("events API contract missing %s", path)
		}
	}
}

package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestGroupsThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("g", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(role string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Group','Tester',now()) RETURNING id`).Scan(&id); err != nil {
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
	admin, adminToken := actor("super_administrator")
	member, memberToken := actor("")
	_, otherToken := actor("")
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
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
	if w := request("POST", "/api/v1/admin/groups", memberToken, `{"name":"Music","description":"Choir","audience":"church","joinPolicy":"approval"}`, "https://api.example"); w.Code != 403 {
		t.Fatalf("member create: %d %s", w.Code, w.Body.String())
	}
	w := request("POST", "/api/v1/admin/groups", adminToken, `{"name":"Music","description":"Choir","audience":"church","joinPolicy":"approval"}`, "https://api.example")
	if w.Code != 201 {
		t.Fatalf("create group: %d %s", w.Code, w.Body.String())
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM groups WHERE name='Music'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", "/api/v1/groups/"+id+"/membership", memberToken, `{}`, "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"pending"`) {
		t.Fatalf("join: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/groups/"+id+"/membership-requests", otherToken, "", ""); w.Code != 403 {
		t.Fatalf("unscoped requests: %d %s", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, admin, id); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "/api/v1/groups/"+id+"/membership-requests", adminToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), member) {
		t.Fatalf("pending requests: %d %s", w.Code, w.Body.String())
	}
	if w = request("PATCH", "/api/v1/groups/"+id+"/membership-requests/"+member, adminToken, `{"decision":"approve","reason":"Reviewed"}`, "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/groups/"+id+"/members", otherToken, "", ""); w.Code != 403 {
		t.Fatalf("nonmember roster: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/groups/"+id+"/members", memberToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"firstName":"Group"`) {
		t.Fatalf("member roster: %d %s", w.Code, w.Body.String())
	}
	leader, leaderToken := actor("")
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, id); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "/api/v1/groups/"+id+"/members", leaderToken, "", ""); w.Code != 200 {
		t.Fatalf("scoped leader roster: %d %s", w.Code, w.Body.String())
	}
	other := request("POST", "/api/v1/admin/groups", adminToken, `{"name":"Prayer Team","description":"Church prayer team","audience":"church","joinPolicy":"approval"}`, "https://api.example")
	if other.Code != 201 {
		t.Fatalf("create second group: %d %s", other.Code, other.Body.String())
	}
	var otherID string
	if err := db.QueryRow(`SELECT id FROM groups WHERE name='Prayer Team'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", "/api/v1/groups/"+otherID+"/members", leaderToken, "", ""); w.Code != 403 {
		t.Fatalf("cross-group leader roster: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/v1/groups", memberToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"active"`) {
		t.Fatalf("list groups: %d %s", w.Code, w.Body.String())
	}
	if w = request("DELETE", "/api/v1/groups/"+id+"/membership", memberToken, "", "https://api.example"); w.Code != 200 {
		t.Fatalf("leave: %d %s", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/v1/groups/"+id+"/membership", memberToken, `{}`, ""); w.Code != 403 {
		t.Fatalf("missing Origin: %d %s", w.Code, w.Body.String())
	}
}

func TestGroupAdministrationThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("g", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(role string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Group','Manager',now()) RETURNING id`).Scan(&id); err != nil {
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
	_, adminToken := actor("super_administrator")
	staff, staffToken := actor("staff")
	_, memberToken := actor("")
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
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

	created := request("POST", "/api/v1/admin/groups", adminToken, `{"name":"Choir","audience":"church","joinPolicy":"approval"}`, "https://api.example")
	if created.Code != 201 {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	var groupID string
	if err := db.QueryRow(`SELECT id FROM groups WHERE name='Choir'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}

	createdByStaff := request("POST", "/api/v1/admin/groups", staffToken, `{"name":"Youth","audience":"church","joinPolicy":"approval"}`, "https://api.example")
	if createdByStaff.Code != 201 {
		t.Fatalf("staff create church group: %d %s", createdByStaff.Code, createdByStaff.Body.String())
	}
	if w := request("GET", "/api/v1/admin/groups", staffToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Choir"`) {
		t.Fatalf("staff managed list: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/admin/groups", memberToken, "", ""); w.Code != 403 {
		t.Fatalf("member managed list: %d %s", w.Code, w.Body.String())
	}
	leader, _ := actor("")
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, groupID); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/admin/groups/"+groupID+"/leaders", staffToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), leader) || strings.Contains(w.Body.String(), "email") {
		t.Fatalf("staff group leader list: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/admin/groups/"+groupID+"/leaders", memberToken, "", ""); w.Code != 403 {
		t.Fatalf("member group leader list: %d %s", w.Code, w.Body.String())
	}
	if w := request("PATCH", "/api/v1/admin/groups/"+groupID, staffToken, `{"status":"archived","reason":"Ministry closed"}`, "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("staff archive group: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/groups", memberToken, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), `"name":"Choir"`) {
		t.Fatalf("archived group remained public: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/admin/groups?includeArchived=true", staffToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"archived":true`) {
		t.Fatalf("archived group missing from admin list: %d %s", w.Code, w.Body.String())
	}
	if w := request("PATCH", "/api/v1/admin/groups/"+groupID, staffToken, `{"status":"active","reason":"Ministry resumed"}`, "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("staff restore group: %d %s", w.Code, w.Body.String())
	}
	if w := request("PATCH", "/api/v1/admin/groups/"+groupID, memberToken, `{"status":"archived","reason":"Unauthorized"}`, "https://api.example"); w.Code != 403 {
		t.Fatalf("member archive group: %d %s", w.Code, w.Body.String())
	}
	var auditCount int
	if err := db.QueryRow(`SELECT count(*) FROM group_audit_events WHERE group_id=$1 AND actor_id=$2 AND action IN ('archived','restored')`, groupID, staff).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("expected archive and restore audits, got %d", auditCount)
	}
}

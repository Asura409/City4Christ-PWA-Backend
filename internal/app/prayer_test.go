package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPrayerRoutesThroughGlobalAppEnforcePrivacy(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("p", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(email, role string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Prayer','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
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
	ownerID, ownerToken := actor("owner@example.invalid", "")
	memberID, memberToken := actor("member@example.invalid", "")
	_, outsiderToken := actor("outsider@example.invalid", "")
	_, adminToken := actor("pastor@example.invalid", "super_administrator")
	var groupID string
	if err := db.QueryRow(`INSERT INTO groups(name,audience,join_policy,created_by) VALUES('Choir','church','open',$1) RETURNING id`, ownerID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active'),($1,$3,'active')`, groupID, ownerID, memberID); err != nil {
		t.Fatal(err)
	}

	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if method != "GET" {
			r.Header.Set("Origin", "https://api.example")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	create := func(token, body string) string {
		t.Helper()
		w := request("POST", "/api/v1/prayer/requests", token, body)
		if w.Code != 201 {
			t.Fatalf("create prayer: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			Data struct {
				ID      string `json:"id"`
				Author  string `json:"author"`
				IsOwner bool   `json:"isOwner"`
				Body    string `json:"body"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data.ID
	}

	if w := request("GET", "/api/v1/prayer/requests", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated collection: %d %s", w.Code, w.Body.String())
	}
	communityID := create(ownerToken, `{"body":"Please pray for my family","visibility":"community","anonymous":true}`)
	privateID := create(ownerToken, `{"body":"Sensitive private request","visibility":"private","anonymous":false}`)
	groupPath := fmt.Sprintf(`{"body":"Prayer for our ministry","visibility":"group","groupId":%q}`, groupID)
	groupRequestID := create(ownerToken, groupPath)
	if w := request("POST", "/api/v1/prayer/requests", outsiderToken, groupPath); w.Code != 403 {
		t.Fatalf("nonmember created a group request: %d %s", w.Code, w.Body.String())
	}

	outsiderFeed := request("GET", "/api/v1/prayer/requests?limit=20", outsiderToken, "")
	if outsiderFeed.Code != 200 || !strings.Contains(outsiderFeed.Body.String(), communityID) || strings.Contains(outsiderFeed.Body.String(), privateID) || strings.Contains(outsiderFeed.Body.String(), groupRequestID) {
		t.Fatalf("outsider collection leaked scoped request: %d %s", outsiderFeed.Code, outsiderFeed.Body.String())
	}
	if !strings.Contains(outsiderFeed.Body.String(), `"author":"Anonymous"`) || strings.Contains(outsiderFeed.Body.String(), ownerID) || strings.Contains(outsiderFeed.Body.String(), "owner@example.invalid") {
		t.Fatalf("anonymous community attribution leaked: %s", outsiderFeed.Body.String())
	}
	memberFeed := request("GET", "/api/v1/prayer/requests?limit=20", memberToken, "")
	if memberFeed.Code != 200 || !strings.Contains(memberFeed.Body.String(), groupRequestID) || strings.Contains(memberFeed.Body.String(), privateID) {
		t.Fatalf("member collection has wrong group/private scope: %d %s", memberFeed.Code, memberFeed.Body.String())
	}
	adminFeed := request("GET", "/api/v1/prayer/requests?limit=20", adminToken, "")
	if adminFeed.Code != 200 || strings.Contains(adminFeed.Body.String(), privateID) || strings.Contains(adminFeed.Body.String(), groupRequestID) {
		t.Fatalf("staff role bypassed prayer privacy: %d %s", adminFeed.Code, adminFeed.Body.String())
	}
	for _, token := range []string{outsiderToken, adminToken} {
		if w := request("GET", "/api/v1/prayer/requests/"+privateID, token, ""); w.Code != 404 {
			t.Fatalf("hidden private detail disclosed to actor: %d %s", w.Code, w.Body.String())
		}
	}
	for _, token := range []string{outsiderToken, adminToken} {
		if w := request("GET", "/api/v1/prayer/requests/"+groupRequestID, token, ""); w.Code != 404 {
			t.Fatalf("nonmember group detail disclosed: %d %s", w.Code, w.Body.String())
		}
	}
	ownerDetail := request("GET", "/api/v1/prayer/requests/"+communityID, ownerToken, "")
	if ownerDetail.Code != 200 || !strings.Contains(ownerDetail.Body.String(), `"isOwner":true`) || !strings.Contains(ownerDetail.Body.String(), `"author":"You"`) || strings.Contains(ownerDetail.Body.String(), `"authorId"`) {
		t.Fatalf("owner detail lacks owner affordance or leaks IDs: %d %s", ownerDetail.Code, ownerDetail.Body.String())
	}

	for i := 0; i < 2; i++ {
		if w := request("PUT", "/api/v1/prayer/requests/"+groupRequestID+"/prayed", memberToken, "{}"); w.Code != 200 {
			t.Fatalf("I-prayed acknowledgement %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	groupDetail := request("GET", "/api/v1/prayer/requests/"+groupRequestID, memberToken, "")
	if groupDetail.Code != 200 || !strings.Contains(groupDetail.Body.String(), `"prayerCount":1`) || !strings.Contains(groupDetail.Body.String(), `"hasPrayed":true`) {
		t.Fatalf("duplicate acknowledgement was not idempotent: %d %s", groupDetail.Code, groupDetail.Body.String())
	}
	if w := request("PATCH", "/api/v1/prayer/requests/"+groupRequestID, memberToken, `{"status":"answered"}`); w.Code != 404 {
		t.Fatalf("nonowner changed prayer status: %d %s", w.Code, w.Body.String())
	}
	if w := request("PATCH", "/api/v1/prayer/requests/"+communityID, ownerToken, `{"status":"answered"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("owner answer: %d %s", w.Code, w.Body.String())
	}
	answered := request("GET", "/api/v1/prayer/requests?status=answered", memberToken, "")
	if answered.Code != 200 || !strings.Contains(answered.Body.String(), communityID) {
		t.Fatalf("answered filter: %d %s", answered.Code, answered.Body.String())
	}
	if w := request("PATCH", "/api/v1/prayer/requests/"+communityID, ownerToken, `{"status":"archived"}`); w.Code != 200 {
		t.Fatalf("owner archive: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/prayer/requests/"+communityID, ownerToken, ""); w.Code != 404 {
		t.Fatalf("archived request remained in detail: %d %s", w.Code, w.Body.String())
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migration rerun: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM prayer_requests WHERE id=$1 AND status='archived'`, communityID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration rerun did not preserve request: count=%d err=%v", count, err)
	}
}

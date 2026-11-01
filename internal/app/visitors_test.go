package app

import (
	"encoding/json"
	"fmt"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/visitors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func visitorHTTP(t *testing.T) *churchHTTPFixture {
	f := newChurchHTTPFixture(t)
	p, e := visitors.NewProtection("v1", map[string][]byte{"v1": make([]byte, 32)})
	if e != nil {
		t.Fatal(e)
	}
	f.handler = New(f.db, Config{AppOrigin: f.origin, VerificationKey: make([]byte, 32), Now: func() time.Time { return f.now }, VisitorIntakeEnabled: true, VisitorProtection: p})
	return f
}
func visitorBody() string {
	return fmt.Sprintf(`{"submissionId":%q,"fullName":"Synthetic Visitor","email":"private@example.invalid","phone":"","consent":true,"policyVersion":%q}`, uuid.NewString(), visitors.PolicyVersion)
}
func TestVisitorIntakeAndFollowUpThroughGlobalApp(t *testing.T) {
	f := visitorHTTP(t)
	staffID, staff := f.account(t, "staff", false)
	_, member := f.account(t, "", false)
	body := visitorBody()
	w := f.request("POST", "/api/v1/visitors", "", body)
	if w.Code != 202 || strings.Contains(w.Body.String(), "private@example.invalid") || strings.Contains(w.Body.String(), "Synthetic Visitor") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/admin/visitors", member, ""); w.Code != 403 {
		t.Fatal("member queue", w.Code)
	}
	if w = f.request("GET", "/api/v1/admin/visitors", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	page := pastoralData[[]visitors.Summary](t, f.request("GET", "/api/v1/admin/visitors", staff, ""), 200)
	if len(page) != 1 {
		t.Fatal(page)
	}
	id := page[0].ID
	d := pastoralData[visitors.Detail](t, f.request("GET", "/api/v1/admin/visitors/"+id, staff, ""), 200)
	if d.Email != "private@example.invalid" || d.ContactVerified {
		t.Fatal(d)
	}
	w = f.request("PATCH", "/api/v1/admin/visitors/"+id, staff, `{"version":1,"status":"contacted"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.request("PATCH", "/api/v1/admin/visitors/"+id, staff, `{"version":1,"status":"closed"}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if _, e := f.db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, staffID); e != nil {
		t.Fatal(e)
	}
	if w = f.request("GET", "/api/v1/admin/visitors/"+id, staff, ""); w.Code != 403 {
		t.Fatal("revoked actor", w.Code)
	}
	var audit string
	if e := f.db.QueryRow(`SELECT string_agg(action,' ') FROM visitor_audit_events`).Scan(&audit); e != nil || !strings.Contains(audit, "viewed") {
		t.Fatal(audit, e)
	}
	var in map[string]any
	if e := json.Unmarshal([]byte(body), &in); e != nil {
		t.Fatal(e)
	}
	in["consent"] = false
	in["submissionId"] = uuid.NewString()
	raw, _ := json.Marshal(in)
	if w = f.request("POST", "/api/v1/visitors", "", string(raw)); w.Code != 422 || !strings.Contains(w.Body.String(), `"consent":`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestVisitorIntakeIsDisabledByDefaultAndRateLimitedWhenEnabled(t *testing.T) {
	f := newChurchHTTPFixture(t)
	w := f.request("GET", "/api/v1/visitors/config", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.request("POST", "/api/v1/visitors", "", visitorBody()); w.Code != 503 {
		t.Fatal(w.Code)
	}
	f = visitorHTTP(t)
	for i := 0; i < 60; i++ {
		if w = f.request("POST", "/api/v1/visitors", "", visitorBody()); w.Code != 202 {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
	if w = f.request("POST", "/api/v1/visitors", "", visitorBody()); w.Code != 429 {
		t.Fatal(w.Code)
	}
}

func TestSharedWelcomeDeskAllowsSeveralVisitorsOnOneIP(t *testing.T) {
	f := visitorHTTP(t)
	for i := 0; i < 6; i++ {
		w := f.request("POST", "/api/v1/visitors", "", visitorBody())
		if w.Code != 202 {
			t.Fatalf("visitor %d behind shared church Wi-Fi: %d", i+1, w.Code)
		}
	}
}

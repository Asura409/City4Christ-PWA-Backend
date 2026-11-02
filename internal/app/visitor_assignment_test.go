package app

import (
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/visitors"
)

func TestVisitorTaskAssignmentThroughGlobalApp(t *testing.T) {
	f := visitorHTTP(t)
	_, staff := f.account(t, "staff", false)
	volunteerID, volunteer := f.account(t, "", false)
	_, other := f.account(t, "", false)
	if w := f.request("POST", "/api/v1/visitors", "", visitorBody()); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	page := pastoralData[[]visitors.Summary](t, f.request("GET", "/api/v1/admin/visitors", staff, ""), 200)
	if len(page) != 1 {
		t.Fatal(page)
	}
	id := page[0].ID
	path := "/api/v1/admin/visitors/" + id + "/assignment"
	body := `{"accountId":"` + volunteerID + `","reason":"Welcome Team follow-up","version":1}`
	if w := f.request("POST", path, other, body); w.Code != 403 {
		t.Fatalf("member assignment %d %s", w.Code, w.Body.String())
	}
	if w := f.request("POST", path, staff, `{"accountId":"`+volunteerID+`","reason":"","version":1}`); w.Code != 422 {
		t.Fatalf("invalid assignment %d %s", w.Code, w.Body.String())
	}
	w := f.request("POST", path, staff, body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), volunteerID) {
		t.Fatalf("assignment %d %s", w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/admin/visitors", volunteer, ""); w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatalf("assigned queue %d %s", w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/admin/visitors/"+id, volunteer, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "private@example.invalid") {
		t.Fatalf("assigned detail %d %s", w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/admin/visitors/"+page[0].ID, other, ""); w.Code != 403 {
		t.Fatalf("unassigned detail %d %s", w.Code, w.Body.String())
	}
	if w = f.request("PATCH", "/api/v1/admin/visitors/"+id, volunteer, `{"version":2,"status":"withdrawn"}`); w.Code != 403 {
		t.Fatalf("volunteer withdrew %d %s", w.Code, w.Body.String())
	}
	if w = f.request("POST", path, staff, `{"accountId":"","reason":"Volunteer unavailable","version":2}`); w.Code != 200 {
		t.Fatalf("unassign %d %s", w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/admin/visitors/"+id, volunteer, ""); w.Code != 403 {
		t.Fatalf("revoked access %d %s", w.Code, w.Body.String())
	}
	var notice string
	if err := f.db.QueryRow(`SELECT body FROM notifications WHERE account_id=$1 AND kind='visitor.assigned'`, volunteerID).Scan(&notice); err != nil || strings.Contains(notice, "private@example.invalid") {
		t.Fatalf("private assignment notice %q %v", notice, err)
	}
}

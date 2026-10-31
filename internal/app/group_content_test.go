package app

import (
	"fmt"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/events"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/posts"
	"strings"
	"testing"
)

func TestGroupContentThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	admin, _ := f.account(t, "super_administrator", false)
	member, token := f.account(t, "", false)
	_, outsider := f.account(t, "", false)
	leader, leadToken := f.account(t, "", false)
	var gid string
	if e := f.db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('Private group','','church','open',$1) RETURNING id`, admin).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, `INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,'` + member + `','active')`, `INSERT INTO account_roles(account_id,role,scope) VALUES('` + leader + `','ministry_leader',$1)`} {
		if _, e := f.db.Exec(q, gid); e != nil {
			t.Fatal(e)
		}
	}
	p := pastoralData[posts.Post](t, f.request("POST", "/api/v1/posts", token, fmt.Sprintf(`{"body":"Private group post","groupId":%q}`, gid)), 201)
	w := f.request("GET", "/api/v1/posts?groupId="+gid, token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), p.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/posts?groupId="+gid, outsider, ""); w.Code != 404 {
		t.Fatal("outsider feed", w.Code)
	}
	ev := pastoralData[events.Event](t, f.request("POST", "/api/v1/admin/events", leadToken, fmt.Sprintf(`{"title":"Group gathering","startsAt":"2030-01-01T12:00:00Z","timeZone":"Africa/Lagos","groupId":%q}`, gid)), 201)
	_ = pastoralData[events.Event](t, f.request("PATCH", "/api/v1/admin/events/"+ev.ID, leadToken, `{"title":"Group gathering","startsAt":"2030-01-01T12:00:00Z","timeZone":"Africa/Lagos","status":"published","version":1}`), 200)
	for _, path := range []string{"/api/v1/events?groupId=" + gid, "/api/v1/events/" + ev.ID + "/calendar.ics", "/api/v1/events/" + ev.ID + "/coordinators"} {
		w = f.request("GET", path, token, "")
		if w.Code != 200 {
			t.Fatal("member", path, w.Code, w.Body.String())
		}
		w = f.request("GET", path, outsider, "")
		if w.Code != 404 {
			t.Fatal("outsider", path, w.Code, w.Body.String())
		}
	}
}

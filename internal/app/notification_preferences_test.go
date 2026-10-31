package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"strings"
	"testing"
)

func TestNotificationPreferencesAndReadAllThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, token := f.account(t, "", false)
	if w := f.request("GET", "/api/v1/notifications/preferences", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := f.request("GET", "/api/v1/notifications/preferences", token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pushEnabled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	body := `{"pushEnabled":true,"groups":false,"events":true,"pastoral":true,"messages":true,"announcements":true}`
	if w = f.request("PUT", "/api/v1/notifications/preferences", token, body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.request("PUT", "/api/v1/notifications/preferences", token, `{}`); w.Code != 422 {
		t.Fatal("incomplete preferences", w.Code)
	}
	if w = f.request("PUT", "/api/v1/notifications/read-all", token, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestBroadcastHTTPRequiresAudienceReviewAndExplicitSend(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, staff := f.account(t, "staff", false)
	_, member := f.account(t, "", false)
	body := `{"title":"Service update","body":"Synthetic test broadcast","audience":"church"}`
	if w := f.request("POST", "/api/v1/admin/broadcasts", member, body); w.Code != 403 {
		t.Fatal(w.Code)
	}
	draft := pastoralData[notifications.Broadcast](t, f.request("POST", "/api/v1/admin/broadcasts", staff, body), 201)
	_ = pastoralData[notifications.Broadcast](t, f.request("GET", "/api/v1/admin/broadcasts/"+draft.ID, staff, ""), 200)
	if w := f.request("POST", "/api/v1/admin/broadcasts/"+draft.ID+"/send", staff, `{"version":1}`); w.Code != 422 {
		t.Fatal("send without confirmation", w.Code)
	}
	sent := pastoralData[notifications.Broadcast](t, f.request("POST", "/api/v1/admin/broadcasts/"+draft.ID+"/send", staff, `{"version":1,"confirm":true}`), 200)
	if sent.Status != "sent" || sent.RecipientCount != 2 {
		t.Fatal(sent)
	}
}

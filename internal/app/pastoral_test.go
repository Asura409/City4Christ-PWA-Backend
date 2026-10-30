package app

import (
	"encoding/json"
	"fmt"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pastoralData[T any](t *testing.T, w *httptest.ResponseRecorder, want int) T {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP %d want %d: %s", w.Code, want, w.Body.String())
	}
	var r struct{ Data T }
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r.Data
}
func TestPastoralApprovalFlowThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, admin := f.account(t, "super_administrator", true)
	pastorID, pastor := f.account(t, "assistant_pastor", false)
	_, member := f.account(t, "", false)
	_, other := f.account(t, "staff", false)
	if w := f.request("GET", "/api/v1/pastors", "", ""); w.Code != 401 {
		t.Fatalf("anonymous directory %d", w.Code)
	}
	body := fmt.Sprintf(`{"accountId":%q,"timeZone":"Africa/Lagos","bio":"Prayer appointments"}`, pastorID)
	if w := f.request("POST", "/api/v1/admin/pastors", other, body); w.Code != 403 {
		t.Fatal("staff appointed pastor", w.Code, w.Body.String())
	}
	p := pastoralData[pastoral.Profile](t, f.request("POST", "/api/v1/admin/pastors", admin, body), 201)
	if p.ID != pastorID {
		t.Fatal(p)
	}
	slotBody := fmt.Sprintf(`{"startsAt":%q,"endsAt":%q}`, f.now.Add(time.Hour).Format(time.RFC3339), f.now.Add(90*time.Minute).Format(time.RFC3339))
	slot := pastoralData[pastoral.Slot](t, f.request("POST", "/api/v1/me/pastoral/availability", pastor, slotBody), 201)
	booking := pastoralData[pastoral.Booking](t, f.request("POST", "/api/v1/pastor-bookings", member, fmt.Sprintf(`{"slotId":%q}`, slot.ID)), 201)
	if booking.Status != "pending" {
		t.Fatal("auto-approved request")
	}
	mine := pastoralData[[]pastoral.Booking](t, f.request("GET", "/api/v1/me/pastor-bookings", other, ""), 200)
	if len(mine) != 0 {
		t.Fatal("staff got private care list")
	}
	if w := f.request("PATCH", "/api/v1/me/pastoral/bookings/"+booking.ID, other, `{"status":"approved","version":1}`); w.Code != 404 {
		t.Fatal("staff approved another appointment", w.Code)
	}
	approved := pastoralData[pastoral.Booking](t, f.request("PATCH", "/api/v1/me/pastoral/bookings/"+booking.ID, pastor, `{"status":"approved","version":1}`), 200)
	if approved.Status != "approved" {
		t.Fatal(approved)
	}
	if w := f.request("POST", "/api/v1/pastor-bookings", member, fmt.Sprintf(`{"slotId":%q,"memberId":%q}`, slot.ID, pastorID)); w.Code != 400 {
		t.Fatal("forged actor accepted", w.Code)
	}
	cancelled := pastoralData[pastoral.Booking](t, f.request("DELETE", "/api/v1/pastor-bookings/"+booking.ID, member, ""), 200)
	if cancelled.Status != "cancelled" {
		t.Fatal(cancelled)
	}
	w := f.request("GET", "/api/v1/pastors", member, "")
	if strings.Contains(w.Body.String(), "email") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("directory privacy/cache regression", w.Body.String())
	}
}
func TestPastoralHTTPValidationOriginAndRateLimits(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, member := f.account(t, "", false)
	for _, path := range []string{"/api/v1/pastors?limit=0", "/api/v1/pastors?cursor=bad", "/api/v1/pastors?limit=1&limit=2", "/api/v1/pastors?memberId=other"} {
		if w := f.request("GET", path, member, ""); w.Code != 422 {
			t.Fatalf("query accepted %s %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/pastor-bookings", strings.NewReader(`{"slotId":"11111111-1111-4111-8111-111111111111"}`))
	r.Header.Set("Authorization", "Bearer "+member)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted origin accepted")
	}
	limited := false
	for range 65 {
		w := f.request("GET", "/api/v1/pastors", member, "")
		if w.Code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("pastoral reads are unbounded")
	}
}

func TestPastoralDirectoryPublicationRequiresApplicationMFA(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, admin := f.account(t, "super_administrator", false)
	pastor, _ := f.account(t, "assistant_pastor", false)
	w := f.request("POST", "/api/v1/admin/pastors", admin, fmt.Sprintf(`{"accountId":%q,"timeZone":"UTC"}`, pastor))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "mfa_required") {
		t.Fatalf("directory publication bypassed MFA: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM pastor_profiles`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed guard created profile", err)
	}
}

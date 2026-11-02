package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const deletionPolicy = "account-deletion-request-v1"
const deletionBody = `{"confirm":true,"policyVersion":"account-deletion-request-v1"}`

func TestAccountDeletionAnonymousRoutes(t *testing.T) {
	f := newChurchHTTPFixture(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/me/deletion-request"}, {"DELETE", "/api/v1/me"},
		{"GET", "/api/v1/admin/account-deletion-requests"},
	} {
		if w := f.request(tc.method, tc.path, "", deletionBody); w.Code != 401 {
			t.Fatalf("missing auth boundary %s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
func TestAccountDeletionRequestAndReviewThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	id, token := f.account(t, "", false)
	_, reviewer := f.account(t, "super_administrator", true)
	_, member := f.account(t, "", false)
	if _, err := f.db.Exec("UPDATE accounts SET directory_visible=true WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	w := f.request("GET", "/api/v1/me/deletion-request", token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), deletionPolicy) || !strings.Contains(w.Body.String(), `"requested":false`) {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if w = f.request("DELETE", "/api/v1/me", token, `{"confirm":false,"policyVersion":"account-deletion-request-v1"}`); w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.request("DELETE", "/api/v1/me", token, deletionBody)
	if w.Code != 202 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"status":"requested"`) {
		t.Fatalf("request: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != refreshCookieName || cookies[0].MaxAge != -1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("cookie not cleared", cookies)
	}
	var result struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.ID == "" {
		t.Fatal("no request id", err, w.Body.String())
	}
	if w = f.request("GET", "/api/v1/me", token, ""); w.Code != 401 {
		t.Fatal("disabled bearer accepted", w.Code, w.Body.String())
	}
	if _, err := f.sessions.Start(context.Background(), id); err == nil {
		t.Fatal("disabled account signed in")
	}
	var hidden bool
	if err := f.db.QueryRow("SELECT NOT directory_visible AND deletion_requested_at IS NOT NULL FROM accounts WHERE id=$1", id).Scan(&hidden); err != nil || !hidden {
		t.Fatal("directory or state", hidden, err)
	}
	path := "/api/v1/admin/account-deletion-requests"
	if w = f.request("GET", path, member, ""); w.Code != 403 {
		t.Fatal("member review allowed", w.Code)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=1&limit=2", "?extra=true"} {
		if bad := f.request("GET", path+query, reviewer, ""); bad.Code != 422 {
			t.Fatalf("bad queue query %s: %d %s", query, bad.Code, bad.Body.String())
		}
	}
	w = f.request("GET", path, reviewer, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "@example.invalid") || !strings.Contains(w.Body.String(), result.Data.ID) {
		t.Fatalf("queue %d %s", w.Code, w.Body.String())
	}
	path += "/" + result.Data.ID
	if w = f.request("GET", path, reviewer, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "@example.invalid") {
		t.Fatalf("detail %d %s", w.Code, w.Body.String())
	}
	if w = f.request("PATCH", path, reviewer, `{"version":1,"status":"reviewing"}`); w.Code != 200 {
		t.Fatalf("review %d %s", w.Code, w.Body.String())
	}
	if w = f.request("PATCH", path, reviewer, `{"version":1,"status":"reviewing"}`); w.Code != 409 {
		t.Fatal("stale review", w.Code, w.Body.String())
	}
	if w = f.request("PATCH", path, reviewer, `{"version":2,"status":"completed"}`); w.Code != 422 {
		t.Fatal("fake deletion completion", w.Code)
	}
	if _, err := f.db.Exec("DELETE FROM account_roles WHERE role='super_administrator'"); err != nil {
		t.Fatal(err)
	}
	if w = f.request("GET", path, reviewer, ""); w.Code != 403 {
		t.Fatal("revoked review allowed", w.Code)
	}
}
func TestAccountDeletionNeedsFreshPrimaryAndEnrolledMFA(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, old := f.account(t, "", false)
	_, enrolled := f.account(t, "", true)
	f.now = f.now.Add(6 * time.Minute)
	if w := f.request("DELETE", "/api/v1/me", old, deletionBody); w.Code != 403 || !strings.Contains(w.Body.String(), "reauthentication_required") {
		t.Fatalf("stale primary %d %s", w.Code, w.Body.String())
	}
	// Start gives a fresh primary proof, but deliberately no factor proof.
	a, err := f.sessions.Authenticate(context.Background(), enrolled)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.sessions.Start(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w := f.request("DELETE", "/api/v1/me", fresh.AccessToken, deletionBody); w.Code != 403 || !strings.Contains(w.Body.String(), "mfa_required") {
		t.Fatalf("missing MFA %d %s", w.Code, w.Body.String())
	}
}
func TestAccountDeletionRejectsUnknownFieldsAndProtectsLastAdministrator(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, super := f.account(t, "super_administrator", true)
	if w := f.request("DELETE", "/api/v1/me", super, `{"confirm":true,"policyVersion":"account-deletion-request-v1","accountId":"other"}`); w.Code != 400 {
		t.Fatal("unknown identity field", w.Code, w.Body.String())
	}
	if w := f.request("DELETE", "/api/v1/me", super, deletionBody); w.Code != 409 || !strings.Contains(w.Body.String(), "last_administrator") {
		t.Fatalf("last admin: %d %s", w.Code, w.Body.String())
	}
}
func TestAccountDeletionAccountLimit(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, token := f.account(t, "", false)
	for i := 0; i < 6; i++ {
		w := f.request("DELETE", "/api/v1/me", token, `{"confirm":false,"policyVersion":"account-deletion-request-v1"}`)
		expected := 422
		if i == 5 {
			expected = 429
		}
		if w.Code != expected {
			t.Fatalf("attempt%d: %d want%d %s", i, w.Code, expected, w.Body.String())
		}
	}
}

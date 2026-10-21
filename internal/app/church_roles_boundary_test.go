package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChurchBoundaryOriginBearerAndJSON(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, admin := f.account(t, "super_administrator", true)
	target, _ := f.account(t, "", true)
	path := "/api/v1/admin/accounts/" + target + "/church-roles"
	body := `{"role":"staff","reason":"Operational appointment"}`
	for _, tc := range []struct {
		name, origin, authorization, cookie, body, content string
		status                                             int
	}{
		{"missing origin", "", "Bearer " + admin, "", body, "application/json", 403},
		{"foreign origin", "https://evil.example.invalid", "Bearer " + admin, "", body, "application/json", 403},
		{"cookie only", f.origin, "", refreshCookieName + "=" + admin, body, "application/json", 401},
		{"unknown field", f.origin, "Bearer " + admin, "", `{"role":"staff","reason":"Appointment","scopeId":"forged"}`, "application/json", 400},
		{"multiple objects", f.origin, "Bearer " + admin, "", body + ` {}`, "application/json", 400},
		{"array", f.origin, "Bearer " + admin, "", `[]`, "application/json", 400},
		{"null", f.origin, "Bearer " + admin, "", `null`, "application/json", 400},
		{"wrong content", f.origin, "Bearer " + admin, "", body, "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", path, strings.NewReader(tc.body))
			r.RemoteAddr = "192.0.2.71:1000"
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			r.Header.Set("Content-Type", tc.content)
			r.Header.Set("Authorization", tc.authorization)
			r.Header.Set("Cookie", tc.cookie)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d want%d %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM account_roles WHERE account_id=$1`, target).Scan(&count); err != nil || count != 0 {
		t.Fatalf("malformed request mutated roles: %d %v", count, err)
	}
}

func TestChurchBoundaryMalformedLookupAndPrivacy(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, admin := f.account(t, "super_administrator", true)
	_, legacy := f.account(t, "access_administrator", true)
	for _, query := range []string{"query=Church&query=Tester", "query=Church&limit=1&limit=2", "query=Church&cursor=&cursor=", "query=%ZZ", "query=Church;limit=1", "query=Church&limit=", "query=Church&limit=+1", "query=Church&unknown=true"} {
		if w := f.request("GET", "/api/v1/admin/role-candidates?"+query, admin, ""); w.Code != 422 {
			t.Fatalf("malformed lookup %q: %d %s", query, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/admin/role-candidates?query=Church", "/api/v1/admin/accounts/11111111-1111-4111-8111-111111111111/church-roles"} {
		w := f.request("GET", path, legacy, "")
		if w.Code != 403 || strings.Contains(w.Body.String(), "@example.invalid") {
			t.Fatalf("legacy private lookup %d %s", w.Code, w.Body.String())
		}
	}
	if _, err := f.db.Exec(`ALTER TABLE mfa_factors RENAME TO unavailable_mfa_factors`); err != nil {
		t.Fatal(err)
	}
	w := f.request("GET", "/api/v1/admin/role-candidates?query=Church", admin, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "@example.invalid") || strings.Contains(w.Body.String(), "mfa_factors") {
		t.Fatalf("storage fault exposed or ignored: %d %s", w.Code, w.Body.String())
	}
}

func TestChurchBoundarySharedIPLimit(t *testing.T) {
	f := newChurchHTTPFixture(t)
	tokens := []string{}
	for range 5 {
		_, token := f.account(t, "staff", true)
		tokens = append(tokens, token)
	}
	for i := 0; i < 120; i++ {
		w := f.request("GET", "/api/v1/admin/role-candidates?query=Church", tokens[i%len(tokens)], "")
		if w.Code != 200 {
			t.Fatalf("request%d blocked early: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := f.request("GET", "/api/v1/admin/role-candidates?query=Church", tokens[0], "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("shared IP limit %d %s", w.Code, w.Body.String())
	}
}

func TestChurchBoundaryResignationAndMFADisable(t *testing.T) {
	f := newChurchHTTPFixture(t)
	first, token := f.account(t, "super_administrator", true)
	body := `{"role":"super_administrator","reason":"Leadership handover"}`
	path := "/api/v1/admin/accounts/" + first + "/church-roles/revoke"
	w := f.request("POST", path, token, body)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "last_administrator") {
		t.Fatalf("final admin %d %s", w.Code, w.Body.String())
	}
	w = f.request("POST", "/api/v1/auth/mfa/disable", token, `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "mfa_protected_role") {
		t.Fatalf("protected factor %d %s", w.Code, w.Body.String())
	}
	f.account(t, "super_administrator", true)
	w = f.request("POST", path, token, body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":true`) {
		t.Fatalf("handover %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM access_audit_events WHERE actor_id=$1 AND target_id=$1 AND role='super_administrator' AND action='revoked' AND reason='Leadership handover'`, first).Scan(&count); err != nil || count != 1 {
		t.Fatalf("self resignation audit %d %v", count, err)
	}
	w = f.request("POST", "/api/v1/auth/mfa/disable", token, `{}`)
	if w.Code != 200 {
		t.Fatalf("factor after resignation %d %s", w.Code, w.Body.String())
	}
	w = f.request("GET", "/api/v1/admin/role-candidates?query=Church", token, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("resigned authority retained %d %s", w.Code, w.Body.String())
	}
}

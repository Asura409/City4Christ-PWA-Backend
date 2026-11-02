package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminMemberSearchThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	staffID, staff := f.account(t, "staff", false)
	_, member := f.account(t, "", false)
	if w := f.request("GET", "/api/v1/admin/members?query=Church&limit=1", "", ""); w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
	if w := f.request("GET", "/api/v1/admin/members?query=Church", member, ""); w.Code != 403 {
		t.Fatalf("member %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?query=Ch", "?query=Church&cursor=no", "?query=Church&limit=99", "?query=Church&roles=true"} {
		if w := f.request("GET", "/api/v1/admin/members"+suffix, staff, ""); w.Code != 422 {
			t.Fatalf("invalid %s: %d %s", suffix, w.Code, w.Body.String())
		}
	}
	w := f.request("GET", "/api/v1/admin/members?query=Church&limit=1", staff, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("search %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data []map[string]json.RawMessage `json:"data"`
		Meta struct {
			NextCursor string `json:"nextCursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || len(payload.Data) != 1 || payload.Meta.NextCursor == "" {
		t.Fatalf("pagination %s %v", w.Body.String(), err)
	}
	for key := range payload.Data[0] {
		if key != "id" && key != "firstName" && key != "lastName" && key != "email" && key != "joinedAt" {
			t.Fatalf("private field %s", key)
		}
	}
	if strings.Contains(w.Body.String(), "mfa") || strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "role") {
		t.Fatalf("security fields leaked: %s", w.Body.String())
	}
	var searchAudit int
	if err := f.db.QueryRow(`SELECT count(*) FROM member_record_audit_events WHERE actor_id=$1 AND action='search_result'`, staffID).Scan(&searchAudit); err != nil || searchAudit != 1 {
		t.Fatalf("search result audit count=%d err=%v", searchAudit, err)
	}
}

func TestAdminMemberDetailThroughGlobalAppIsAuditedAndStaffOnly(t *testing.T) {
	f := newChurchHTTPFixture(t)
	staffID, staff := f.account(t, "staff", false)
	_, member := f.account(t, "", false)
	target, _ := f.account(t, "", false)
	path := "/api/v1/admin/members/" + target
	if w := f.request("GET", path, "", ""); w.Code != 401 {
		t.Fatalf("anonymous member detail: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("GET", path, member, ""); w.Code != 403 {
		t.Fatalf("member detail permission: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("GET", path+"?includeRoles=true", staff, ""); w.Code != 422 {
		t.Fatalf("query parameter accepted: %d %s", w.Code, w.Body.String())
	}
	w := f.request("GET", path, staff, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "@example.invalid") {
		t.Fatalf("staff detail %d %s", w.Code, w.Body.String())
	}
	for _, secret := range []string{"passwordHash", "password", "mfa", "session", "roles"} {
		if strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(secret)) {
			t.Fatalf("member detail leaked %s: %s", secret, w.Body.String())
		}
	}
	var audits int
	var fields string
	if err := f.db.QueryRow(`SELECT count(*),coalesce(max(array_to_string(sensitive_fields,',')),'') FROM member_record_audit_events WHERE actor_id=$1 AND member_id=$2 AND action='detail_view'`, staffID, target).Scan(&audits, &fields); err != nil || audits != 1 || fields != "email" {
		t.Fatalf("missing member detail audit count=%d fields=%q err=%v", audits, fields, err)
	}
	if _, err := f.db.Exec(`ALTER TABLE member_record_audit_events ADD CONSTRAINT test_reject_detail_audit CHECK(action<>'detail_view') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if w = f.request("GET", path, staff, ""); w.Code != 503 || strings.Contains(w.Body.String(), "@example.invalid") {
		t.Fatalf("detail disclosed without audit %d %s", w.Code, w.Body.String())
	}
	if _, err := f.db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	if w = f.request("GET", path, staff, ""); w.Code != 404 || strings.Contains(w.Body.String(), "@example.invalid") {
		t.Fatalf("unverified member disclosed %d %s", w.Code, w.Body.String())
	}
}

func TestSuperAdminCanReviewMemberRecordAccessAudit(t *testing.T) {
	f := newChurchHTTPFixture(t)
	superID, super := f.account(t, "super_administrator", false)
	staffID, staff := f.account(t, "staff", false)
	target, _ := f.account(t, "", false)

	if w := f.request("GET", "/api/v1/admin/members/"+target, staff, ""); w.Code != 200 {
		t.Fatalf("create audited detail read: %d %s", w.Code, w.Body.String())
	}
	path := "/api/v1/admin/members/" + target + "/access-audit"
	if w := f.request("GET", path, "", ""); w.Code != 401 {
		t.Fatalf("anonymous audit access: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("GET", path, staff, ""); w.Code != 403 {
		t.Fatalf("staff audit access: %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?limit=101", "?cursor=0", "?cursor=10&other=true"} {
		if w := f.request("GET", path+suffix, super, ""); w.Code != 422 {
			t.Fatalf("invalid audit options %s: %d %s", suffix, w.Code, w.Body.String())
		}
	}
	w := f.request("GET", path+"?limit=10", super, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("super admin audit access: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data []struct {
			ActorID         string   `json:"actorId"`
			Action          string   `json:"action"`
			SensitiveFields []string `json:"sensitiveFields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || len(payload.Data) != 1 {
		t.Fatalf("audit response %s: %v", w.Body.String(), err)
	}
	entry := payload.Data[0]
	if entry.ActorID != staffID || entry.Action != "detail_view" || len(entry.SensitiveFields) != 1 || entry.SensitiveFields[0] != "email" {
		t.Fatalf("wrong audit entry: %#v", entry)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "password") || strings.Contains(strings.ToLower(w.Body.String()), "token") {
		t.Fatalf("sensitive account data leaked: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "@example.invalid") {
		t.Fatalf("email value leaked into access history: %s", w.Body.String())
	}
	var views int
	if err := f.db.QueryRow(`SELECT count(*) FROM member_record_audit_events WHERE actor_id=$1 AND member_id=$2 AND action='audit_view'`, superID, target).Scan(&views); err != nil || views != 1 {
		t.Fatalf("audit inspection not recorded: count=%d err=%v", views, err)
	}
}

func TestStaffCanCorrectOnlyMemberNamesWithReasonAndVersion(t *testing.T) {
	f := newChurchHTTPFixture(t)
	staffID, staff := f.account(t, "staff", false)
	_, member := f.account(t, "", false)
	target, targetToken := f.account(t, "", false)
	path := "/api/v1/admin/members/" + target
	body := `{"firstName":"Grace","reason":"Member requested a name correction","expectedVersion":1}`
	if w := f.request("PATCH", path, "", body); w.Code != 401 {
		t.Fatalf("anonymous correction: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("PATCH", path, member, body); w.Code != 403 {
		t.Fatalf("member correction: %d %s", w.Code, w.Body.String())
	}
	w := f.request("PATCH", path, staff, body)
	if w.Code != 200 {
		t.Fatalf("staff correction: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data["firstName"] != "Grace" || response.Data["version"] != float64(2) {
		t.Fatalf("corrected member response %s: %v", w.Body.String(), err)
	}
	for _, invalid := range []struct {
		body   string
		status int
	}{
		{`{"firstName":"","reason":"Member request","expectedVersion":2}`, 422},
		{`{"email":"takeover@example.invalid","reason":"Member request","expectedVersion":2}`, 400},
		{`{"lastName":"Updated","reason":"","expectedVersion":2}`, 422},
	} {
		if response := f.request("PATCH", path, staff, invalid.body); response.Code != invalid.status {
			t.Fatalf("accepted invalid member correction %s: %d %s", invalid.body, response.Code, response.Body.String())
		}
	}
	if stale := f.request("PATCH", path, staff, `{"lastName":"Updated","reason":"Stale request","expectedVersion":1}`); stale.Code != 409 {
		t.Fatalf("stale correction: %d %s", stale.Code, stale.Body.String())
	}
	if _, err := f.db.Exec(`ALTER TABLE member_record_audit_events ADD CONSTRAINT test_reject_name_update CHECK(action<>'name_update') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if rejected := f.request("PATCH", path, staff, `{"lastName":"Should Roll Back","reason":"Audit failure test","expectedVersion":2}`); rejected.Code != 503 {
		t.Fatalf("correction without audit should fail closed: %d %s", rejected.Code, rejected.Body.String())
	}
	var lastName string
	var version int
	if err := f.db.QueryRow(`SELECT last_name,profile_version FROM accounts WHERE id=$1`, target).Scan(&lastName, &version); err != nil || lastName != "Tester" || version != 2 {
		t.Fatalf("unaudited correction was not rolled back: name=%q version=%d err=%v", lastName, version, err)
	}
	if _, err := f.db.Exec(`ALTER TABLE member_record_audit_events DROP CONSTRAINT test_reject_name_update`); err != nil {
		t.Fatal(err)
	}
	if own := f.request("PATCH", "/api/v1/me", targetToken, `{"lastName":"Member Updated"}`); own.Code != 200 {
		t.Fatalf("member own-profile update: %d %s", own.Code, own.Body.String())
	}
	if stale := f.request("PATCH", path, staff, `{"lastName":"Older Staff Value","reason":"Another request","expectedVersion":2}`); stale.Code != 409 {
		t.Fatalf("staff update did not notice member profile edit: %d %s", stale.Code, stale.Body.String())
	}
	var audits int
	var action, fields, reason string
	if err := f.db.QueryRow(`SELECT count(*),max(action),max(array_to_string(sensitive_fields,',')),max(reason) FROM member_record_audit_events WHERE actor_id=$1 AND member_id=$2`, staffID, target).Scan(&audits, &action, &fields, &reason); err != nil || audits != 1 || action != "name_update" || fields != "firstName" || reason != "Member requested a name correction" {
		t.Fatalf("member correction audit count=%d action=%q fields=%q reason=%q err=%v", audits, action, fields, reason, err)
	}
}

package access

import "testing"

func TestDevotionalEditorialPolicy(t *testing.T) {
	allowed := map[string]bool{"super_administrator": true, "assistant_pastor": true, "staff": true, "content_editor": true, "system_administrator": true}
	for _, role := range []string{"super_administrator", "assistant_pastor", "staff", "content_editor", "system_administrator", "access_administrator", "finance_officer", "ministry_leader", "member"} {
		got := false
		for _, c := range capabilitiesFor(Assignment{Role: role}) {
			if c.Permission == "devotionals.manage" && c.Scope == "" {
				got = true
			}
		}
		if got != allowed[role] {
			t.Fatalf("devotional authority for %s: %v", role, got)
		}
	}
	if len(rolesForPermission("devotionals.manage", "11111111-1111-4111-8111-111111111111")) != 0 {
		t.Fatal("global editorial permission granted at ministry scope")
	}
}

package access

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCurrentAssignmentRepository(t *testing.T) {
	db, _, admin, member := fixture(t)
	ctx := context.Background()
	repo := NewRepository(db)
	got, err := repo.Assignments(ctx, admin)
	if err != nil || !reflect.DeepEqual(got, []Assignment{{Role: "access_administrator", Scope: ""}}) {
		t.Fatalf("assignments=%v err=%v", got, err)
	}
	got, err = repo.Assignments(ctx, member)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("role-free account=%v err=%v", got, err)
	}
	for _, id := range []string{"not-an-id", "00000000-0000-0000-0000-000000000000", "33333333-3333-4333-8333-333333333333"} {
		if _, err := repo.Assignments(ctx, id); !errors.Is(err, ErrForbidden) {
			t.Fatalf("invalid/absent account accepted: %v", err)
		}
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=NULL WHERE id=$1", member); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Assignments(ctx, member); !errors.Is(err, ErrForbidden) {
		t.Fatal("unverified account accepted", err)
	}
}

func TestCurrentEffectiveAccess(t *testing.T) {
	db, s, admin, member := fixture(t)
	ctx := context.Background()
	current, err := s.Current(ctx, member)
	if err != nil || current.Roles == nil || current.Capabilities == nil || len(current.Roles) != 0 || len(current.Capabilities) != 0 {
		t.Fatal("role-free snapshot", current, err)
	}
	a, b := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	if _, err = db.Exec("INSERT INTO access_scopes(id,kind) VALUES($1,'ministry'),($2,'ministry')", a, b); err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []Assignment{{Role: "content_editor"}, {Role: "system_administrator"}, {Role: "access_administrator"}, {Role: "ministry_leader", Scope: b}, {Role: "ministry_leader", Scope: a}} {
		if _, err = db.Exec("INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,$3)", member, assignment.Role, assignment.Scope); err != nil {
			t.Fatal(err)
		}
	}
	current, err = s.Current(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	expected := []Capability{{Permission: "devotionals.manage", Scope: ""}, {Permission: "groups.content.moderate", Scope: a}, {Permission: "groups.content.moderate", Scope: b}, {Permission: "ministry.members.manage", Scope: a}, {Permission: "ministry.members.manage", Scope: b}, {Permission: "prayer.moderate", Scope: a}, {Permission: "prayer.moderate", Scope: b}, {Permission: "roles.manage", Scope: ""}, {Permission: "sermons.manage", Scope: ""}}
	if len(current.Roles) != 5 || !reflect.DeepEqual(current.Capabilities, expected) {
		t.Fatalf("incorrect scoped/deduplicated snapshot: %+v", current)
	}
	for _, capability := range current.Capabilities {
		if err = s.Check(ctx, member, capability.Permission, capability.Scope); err != nil {
			t.Fatal("snapshot disagrees with server authorization", err)
		}
	}
	for _, check := range []Capability{{Permission: "ministry.members.manage", Scope: ""}, {Permission: "ministry.members.manage", Scope: "33333333-3333-4333-8333-333333333333"}, {Permission: "sermons.manage", Scope: a}, {Permission: "finance.individual.read"}} {
		if err = s.Check(ctx, member, check.Permission, check.Scope); !errors.Is(err, ErrForbidden) {
			t.Fatalf("scope/unknown permission accepted: %+v %v", check, err)
		}
	}
	if changed, err := s.Revoke(ctx, admin, member, Change{Role: "content_editor", Reason: "End editorial appointment"}); err != nil || !changed {
		t.Fatal(err)
	}
	current, err = s.Current(ctx, member)
	if err != nil || !reflect.DeepEqual(current.Capabilities, expected) {
		t.Fatal("independent system assignment lost its permission", err)
	}
	if _, err = db.Exec("DELETE FROM account_roles WHERE account_id=$1", member); err != nil {
		t.Fatal(err)
	}
	current, err = s.Current(ctx, member)
	if err != nil || len(current.Roles) != 0 || len(current.Capabilities) != 0 {
		t.Fatal("revoked permissions retained", err)
	}
	// Corrupt/unsupported assignments must not become effective privileges or tags.
	if _, err = db.Exec("ALTER TABLE account_roles DROP CONSTRAINT account_roles_role_scope"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO account_roles(account_id,role,scope) VALUES($1,'super_administrator',$2),($1,'content_editor',$2)", member, a); err != nil {
		t.Fatal(err)
	}
	current, err = s.Current(ctx, member)
	if err != nil || len(current.Roles) != 0 || len(current.Capabilities) != 0 {
		t.Fatal("unsupported role/scope gained effective access", current, err)
	}
	for _, role := range []string{"super_administrator", "assistant_pastor", "staff", "finance_officer"} {
		change := Change{Role: role, Reason: "Must remain protected"}
		if _, err = s.Grant(ctx, admin, member, change); !errors.Is(err, ErrInvalid) {
			t.Fatalf("privileged grant %s opened: %v", role, err)
		}
		if _, err = s.Revoke(ctx, admin, member, change); !errors.Is(err, ErrInvalid) {
			t.Fatalf("privileged revoke %s opened: %v", role, err)
		}
	}
}

func TestPrayerModerationCapabilityRoleScopes(t *testing.T) {
	global := rolesForPermission("prayer.moderate", "")
	expected := []string{"super_administrator", "assistant_pastor", "staff"}
	if !reflect.DeepEqual(global, expected) {
		t.Fatalf("global prayer moderation roles=%v, want %v", global, expected)
	}
	if roles := rolesForPermission("prayer.moderate", "11111111-1111-4111-8111-111111111111"); !reflect.DeepEqual(roles, []string{"ministry_leader"}) {
		t.Fatalf("group prayer moderation roles=%v", roles)
	}
}

func TestCommunityContentModerationCapabilityScopes(t *testing.T) {
	global := rolesForPermission("groups.content.moderate", "")
	expected := []string{"super_administrator", "assistant_pastor", "staff"}
	if !reflect.DeepEqual(global, expected) {
		t.Fatalf("global content moderation roles=%v, want %v", global, expected)
	}
	if roles := rolesForPermission("groups.content.moderate", "11111111-1111-4111-8111-111111111111"); !reflect.DeepEqual(roles, []string{"ministry_leader"}) {
		t.Fatalf("ministry content moderation roles=%v", roles)
	}
}

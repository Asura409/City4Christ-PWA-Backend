package access

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// These matrix tests extend regression coverage for the implemented policy;
// the service's initial missing-method red phase is in church_test.go history.
func TestChurchProtectedRoleMatrix(t *testing.T) {
	db, s, _, target := fixture(t)
	ctx := context.Background()
	super := account(t, db)
	churchMFA(t, db, super)
	churchMFA(t, db, target)
	if err := s.BootstrapChurchAdmin(ctx, super, "Initial leadership"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"super_administrator", "assistant_pastor", "staff", "finance_officer"} {
		t.Run(role, func(t *testing.T) {
			change := Change{Role: role, Reason: "Leadership matrix"}
			if ok, err := s.ChurchGrant(ctx, super, target, change, churchGuard); err != nil || !ok {
				t.Fatalf("super grant %v %v", ok, err)
			}
			if ok, err := s.ChurchGrant(ctx, super, target, change, churchGuard); err != nil || ok {
				t.Fatalf("duplicate grant %v %v", ok, err)
			}
			if ok, err := s.ChurchRevoke(ctx, super, target, change, churchGuard); err != nil || !ok {
				t.Fatalf("super revoke %v %v", ok, err)
			}
			for _, state := range []string{"unverified", "withoutMFA"} {
				bad := account(t, db)
				if state == "unverified" {
					churchMFA(t, db, bad)
					if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, bad); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.ChurchGrant(ctx, super, bad, change, churchGuard); !errors.Is(err, ErrInvalid) {
					t.Fatalf("%s target admitted: %v", state, err)
				}
			}
		})
	}
}

func TestChurchOperationalDelegationMatrix(t *testing.T) {
	db, s, _, target := fixture(t)
	ctx := context.Background()
	super := account(t, db)
	churchMFA(t, db, super)
	if err := s.BootstrapChurchAdmin(ctx, super, "Initial leadership"); err != nil {
		t.Fatal(err)
	}
	a, b := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	if _, err := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry'),($2,'ministry')`, a, b); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"assistant_pastor", "staff"} {
		t.Run(role, func(t *testing.T) {
			actor := account(t, db)
			churchMFA(t, db, actor)
			if _, err := s.ChurchGrant(ctx, super, actor, Change{Role: role, Reason: "Operations appointment"}, churchGuard); err != nil {
				t.Fatal(err)
			}
			ministry := Change{Role: "ministry_leader", Scope: a, Reason: "Youth appointment"}
			if ok, err := s.ChurchGrant(ctx, actor, target, ministry, churchGuard); err != nil || !ok {
				t.Fatalf("ministry delegation %v %v", ok, err)
			}
			if err := s.Check(ctx, target, "ministry.members.manage", a); err != nil {
				t.Fatal(err)
			}
			for _, scope := range []string{"", b} {
				if err := s.Check(ctx, target, "ministry.members.manage", scope); !errors.Is(err, ErrForbidden) {
					t.Fatalf("scope leak %q %v", scope, err)
				}
			}
			for _, protected := range []string{"super_administrator", "assistant_pastor", "staff", "finance_officer"} {
				in := Change{Role: protected, Reason: "Attempted promotion"}
				if _, err := s.ChurchGrant(ctx, actor, target, in, churchGuard); !errors.Is(err, ErrForbidden) {
					t.Fatalf("%s granted %s: %v", role, protected, err)
				}
				if _, err := s.ChurchRevoke(ctx, actor, target, in, churchGuard); !errors.Is(err, ErrForbidden) {
					t.Fatalf("%s revoked %s: %v", role, protected, err)
				}
			}
			for _, legacy := range []string{"access_administrator", "system_administrator"} {
				in := Change{Role: legacy, Reason: "Attempted legacy promotion"}
				if _, err := s.ChurchGrant(ctx, actor, target, in, churchGuard); !errors.Is(err, ErrInvalid) {
					t.Fatalf("church legacy path %s: %v", legacy, err)
				}
				if _, err := s.Grant(ctx, actor, target, in); !errors.Is(err, ErrForbidden) {
					t.Fatalf("legacy authority %s: %v", legacy, err)
				}
			}
			for _, in := range []Change{{Role: "unknown", Reason: "Invalid role"}, {Role: "ministry_leader", Reason: "Global scope"}, {Role: "ministry_leader", Scope: "33333333-3333-4333-8333-333333333333", Reason: "Unregistered scope"}, {Role: "ministry_leader", Scope: "11111111111141118111111111111111", Reason: "Noncanonical scope"}, {Role: "staff", Scope: a, Reason: "Mismatched scope"}} {
				if _, err := s.ChurchGrant(ctx, actor, target, in, churchGuard); !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid assignment %+v: %v", in, err)
				}
			}
			if _, err := s.ChurchGrant(ctx, target, super, Change{Role: "ministry_leader", Scope: b, Reason: "Leader escalation"}, churchGuard); !errors.Is(err, ErrForbidden) {
				t.Fatalf("leader delegated: %v", err)
			}
			if ok, err := s.ChurchRevoke(ctx, actor, target, ministry, churchGuard); err != nil || !ok {
				t.Fatalf("operational revoke %v %v", ok, err)
			}
			guardErr := errors.New("expired proof")
			if _, err := s.ChurchRevoke(ctx, actor, target, ministry, func(context.Context, *sql.Tx) error { return guardErr }); !errors.Is(err, guardErr) {
				t.Fatalf("duplicate revoke bypassed proof: %v", err)
			}
			if _, err := s.ChurchRevoke(ctx, actor, target, ministry, nil); !errors.Is(err, ErrForbidden) {
				t.Fatalf("nil proof duplicate revoke: %v", err)
			}
			if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ChurchGrant(ctx, actor, target, ministry, churchGuard); !errors.Is(err, ErrForbidden) {
				t.Fatalf("unverified actor: %v", err)
			}
			if _, err := db.Exec(`UPDATE accounts SET email_verified_at=now() WHERE id=$1;`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ChurchRevoke(ctx, super, actor, Change{Role: role, Reason: "End appointment"}, churchGuard); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ChurchGrant(ctx, actor, target, ministry, churchGuard); !errors.Is(err, ErrForbidden) {
				t.Fatalf("revoked actor: %v", err)
			}
		})
	}
}

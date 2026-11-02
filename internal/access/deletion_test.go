package access

import (
	"context"
	"errors"
	"testing"
)

func TestPendingDeletionHasNoCapabilitiesOrAppointments(t *testing.T) {
	db, s, actor, target := fixture(t)
	ctx := context.Background()
	super := account(t, db)
	churchMFA(t, db, super)
	if e := s.BootstrapChurchAdmin(ctx, super, "Owner approval"); e != nil {
		t.Fatal(e)
	}
	if e := s.Check(ctx, super, "accounts.deletion.review", ""); e != nil {
		t.Fatal("super admin deletion review", e)
	}
	if _, e := db.Exec(`UPDATE accounts SET deletion_requested_at=now() WHERE id=$1`, actor); e != nil {
		t.Fatal(e)
	}
	if e := s.Check(ctx, actor, "roles.manage", ""); !errors.Is(e, ErrForbidden) {
		t.Fatal("pending actor permission", e)
	}
	if _, e := s.Current(ctx, actor); !errors.Is(e, ErrForbidden) {
		t.Fatal("pending snapshot", e)
	}
	if _, e := db.Exec(`UPDATE accounts SET deletion_requested_at=now() WHERE id=$1`, target); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ChurchGrant(ctx, super, target, Change{Role: "ministry_leader", Scope: "11111111-1111-4111-8111-111111111111", Reason: "Appoint"}, churchGuard); !errors.Is(e, ErrInvalid) {
		t.Fatal("pending church target", e)
	}
	page, e := s.Candidates(ctx, super, "Access", "", 50)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range page.Data {
		if c.ID == target || c.ID == actor {
			t.Fatal("pending candidate exposed")
		}
	}
}

package access

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func churchGuard(context.Context, *sql.Tx) error { return nil }
func churchMFA(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.Exec(`WITH s AS (INSERT INTO auth_sessions(id,account_id,expires_at) VALUES(gen_random_uuid(),$1,now()+interval '1 day') RETURNING id) INSERT INTO mfa_factors(account_id,id,sealed_secret,enrollment_session_id,pending_expires_at,confirmed_at) SELECT $1,gen_random_uuid(),'secret'::bytea,id,now(),now() FROM s`, id)
	if err != nil {
		t.Fatal(err)
	}
}
func TestChurchProtectedDelegation(t *testing.T) {
	db, s, legacy, target := fixture(t)
	ctx := context.Background()
	super := account(t, db)
	churchMFA(t, db, super)
	if err := s.BootstrapChurchAdmin(ctx, super, "Initial appointment"); err != nil {
		t.Fatal(err)
	}
	if err := s.BootstrapChurchAdmin(ctx, target, "Second appointment"); !errors.Is(err, ErrBootstrapConflict) {
		t.Fatalf("duplicate bootstrap: %v", err)
	}
	in := Change{Role: "staff", Reason: "Church operations"}
	if _, err := s.ChurchGrant(ctx, legacy, target, in, churchGuard); !errors.Is(err, ErrForbidden) {
		t.Fatalf("legacy escalation: %v", err)
	}
	if _, err := s.ChurchGrant(ctx, super, target, in, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nil guard: %v", err)
	}
	if _, err := s.ChurchGrant(ctx, super, target, in, churchGuard); !errors.Is(err, ErrInvalid) {
		t.Fatalf("without MFA: %v", err)
	}
	churchMFA(t, db, target)
	if ok, err := s.ChurchGrant(ctx, super, target, in, churchGuard); err != nil || !ok {
		t.Fatalf("grant: %v %v", ok, err)
	}
	if err := s.Check(ctx, target, "roles.operational.manage", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(ctx, target, "roles.privileged.manage", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("staff escalation: %v", err)
	}
	if _, err := s.ChurchRevoke(ctx, super, super, Change{Role: "super_administrator", Reason: "Retire"}, churchGuard); !errors.Is(err, ErrLastAdministrator) {
		t.Fatalf("last admin: %v", err)
	}
	assignments, err := s.ChurchList(ctx, target, super)
	if err != nil || len(assignments) != 1 {
		t.Fatalf("list %v %v", assignments, err)
	}
	page, err := s.Candidates(ctx, target, "Access", "", 1)
	if err != nil || len(page.Data) != 1 || page.NextCursor == "" {
		t.Fatalf("candidates %+v %v", page, err)
	}
	if _, err = s.Candidates(ctx, legacy, "Access", "", 10); !errors.Is(err, ErrForbidden) {
		t.Fatalf("legacy directory: %v", err)
	}
}

func TestChurchGuardRollbackAndDirectory(t *testing.T) {
	db, s, _, target := fixture(t)
	ctx := context.Background()
	super := account(t, db)
	churchMFA(t, db, super)
	churchMFA(t, db, target)
	if err := s.BootstrapChurchAdmin(ctx, super, "Owner appointment"); err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("expired authentication")
	in := Change{Role: "finance_officer", Reason: "Finance reporting"}
	if _, err := s.ChurchGrant(ctx, super, target, in, func(context.Context, *sql.Tx) error { return rejected }); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM account_roles WHERE account_id=$1`, target).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed guard persisted: %d %v", count, err)
	}
	if _, err := s.ChurchGrant(ctx, super, target, in, churchGuard); err != nil {
		t.Fatal(err)
	}
	current, err := s.Current(ctx, target)
	if err != nil || len(current.Roles) != 1 || len(current.Capabilities) != 0 {
		t.Fatalf("finance tag %+v %v", current, err)
	}
	if _, err = s.Candidates(ctx, super, "Access", "bad", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.Candidates(ctx, super, "Ac", "", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	page, err := s.Candidates(ctx, super, "%%%", "", 10)
	if err != nil || len(page.Data) != 0 {
		t.Fatalf("wildcard injection %+v %v", page, err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	page, err = s.Candidates(ctx, super, "Access", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range page.Data {
		if c.ID == target {
			t.Fatal("unverified directory leak")
		}
	}
	if _, err = s.ChurchRevoke(ctx, super, target, in, churchGuard); err != nil {
		t.Fatalf("must revoke unverified: %v", err)
	}
}
func TestChurchConcurrentSuperResignation(t *testing.T) {
	db, s, _, b := fixture(t)
	ctx := context.Background()
	a := account(t, db)
	churchMFA(t, db, a)
	churchMFA(t, db, b)
	if err := s.BootstrapChurchAdmin(ctx, a, "Owner appointment"); err != nil {
		t.Fatal(err)
	}
	in := Change{Role: "super_administrator", Reason: "Leadership handover"}
	if _, err := s.ChurchGrant(ctx, a, b, in, churchGuard); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{a, b} {
		go func(id string) { <-start; _, err := s.ChurchRevoke(ctx, id, id, in, churchGuard); results <- err }(id)
	}
	close(start)
	success, last := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrLastAdministrator) {
			last++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || last != 1 {
		t.Fatalf("resignations %d last %d", success, last)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM account_roles WHERE role='super_administrator'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("remaining %d %v", count, err)
	}
}

func TestChurchPeerRevocationIsImmediate(t *testing.T) {
	db, s, _, target := fixture(t)
	ctx := context.Background()
	a := account(t, db)
	b := account(t, db)
	churchMFA(t, db, a)
	churchMFA(t, db, b)
	churchMFA(t, db, target)
	if err := s.BootstrapChurchAdmin(ctx, a, "Owner appointment"); err != nil {
		t.Fatal(err)
	}
	in := Change{Role: "super_administrator", Reason: "Leadership handover"}
	if _, err := s.ChurchGrant(ctx, a, b, in, churchGuard); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChurchRevoke(ctx, b, a, in, churchGuard); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChurchGrant(ctx, a, target, Change{Role: "staff", Reason: "Revoked leader"}, churchGuard); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked admin acted: %v", err)
	}
	if _, err := s.ChurchGrant(ctx, b, b, Change{Role: "staff", Reason: "Self promotion"}, churchGuard); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self grant: %v", err)
	}
	if _, err := s.ChurchRevoke(ctx, b, b, Change{Role: "staff", Reason: "Unsupported self change"}, churchGuard); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other self revoke: %v", err)
	}
}

func TestChurchAuditFailureRollsBack(t *testing.T) {
	db, s, _, target := fixture(t)
	ctx := context.Background()
	a := account(t, db)
	churchMFA(t, db, a)
	churchMFA(t, db, target)
	if err := s.BootstrapChurchAdmin(ctx, a, "Owner appointment"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION reject_church_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_church_audit BEFORE INSERT ON access_audit_events FOR EACH ROW EXECUTE FUNCTION reject_church_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChurchGrant(ctx, a, target, Change{Role: "staff", Reason: "Appointment"}, churchGuard); err == nil {
		t.Fatal("unaudited assignment")
	}
	rows, err := s.ChurchList(ctx, a, target)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rolledback rows %v %v", rows, err)
	}
}

func TestChurchGuardRunsAfterTargetLock(t *testing.T) {
	db, s, _, target := fixture(t)
	ctx := context.Background()
	a := account(t, db)
	churchMFA(t, db, a)
	churchMFA(t, db, target)
	if err := s.BootstrapChurchAdmin(ctx, a, "Owner appointment"); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.Exec(`SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, target); err != nil {
		t.Fatal(err)
	}
	var expired atomic.Bool
	rejected := errors.New("proof expired while waiting")
	result := make(chan error, 1)
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() {
		_, e := s.ChurchGrant(bounded, a, target, Change{Role: "staff", Reason: "Appointment"}, func(context.Context, *sql.Tx) error {
			if expired.Load() {
				return rejected
			}
			return nil
		})
		result <- e
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM accounts WHERE id=%FOR SHARE%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("role transaction never waited for target")
		}
		time.Sleep(10 * time.Millisecond)
	}
	expired.Store(true)
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, rejected) {
		t.Fatalf("stale proof accepted: %v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM account_roles WHERE account_id=$1`, target).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale grant persisted %d %v", count, err)
	}
}

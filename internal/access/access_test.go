package access

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func fixture(t *testing.T) (*sql.DB, *Service, string, string) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	actor := account(t, db)
	target := account(t, db)
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'access_administrator')`, actor); err != nil {
		t.Fatal(err)
	}
	return db, NewService(db), actor, target
}
func account(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Access','Test',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestDelegationAndScopes(t *testing.T) {
	db, s, admin, target := fixture(t)
	ctx := context.Background()
	in := Change{Role: "content_editor", Reason: "Appointed by church leadership"}
	if _, err := s.Grant(ctx, target, admin, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member delegated: %v", err)
	}
	if _, err := s.Grant(ctx, admin, admin, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self grant: %v", err)
	}
	for _, role := range []string{"access_administrator", "system_administrator", "finance_administrator", "pastor", "unknown"} {
		bad := in
		bad.Role = role
		if _, err := s.Grant(ctx, admin, target, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("privileged role %s: %v", role, err)
		}
	}
	granted, err := s.Grant(ctx, admin, target, in)
	if err != nil || !granted {
		t.Fatalf("grant: %v %v", granted, err)
	}
	if err = s.Check(ctx, target, "sermons.manage", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, target, "roles.manage", ""); !errors.Is(err, ErrForbidden) {
		t.Fatal("editor can delegate")
	}
	if changed, err := s.Grant(ctx, admin, target, in); err != nil || changed {
		t.Fatal("duplicate grant not idempotent")
	}
	a, b := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	if _, err = db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry'),($2,'ministry')`, a, b); err != nil {
		t.Fatal(err)
	}
	leader := Change{Role: "ministry_leader", Scope: a, Reason: "Youth ministry appointment"}
	if _, err = s.Grant(ctx, admin, target, leader); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, target, "ministry.members.manage", a); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{b, ""} {
		if err = s.Check(ctx, target, "ministry.members.manage", scope); !errors.Is(err, ErrForbidden) {
			t.Fatal("scope leak")
		}
	}
	if err = s.Check(ctx, admin, "sermons.manage", ""); !errors.Is(err, ErrForbidden) {
		t.Fatal("access admin inherited editorial powers")
	}
	if err = s.Check(ctx, target, "unknown", a); !errors.Is(err, ErrForbidden) {
		t.Fatal("unknown permission allowed")
	}
	if _, err = s.Revoke(ctx, admin, target, in); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, target, "sermons.manage", ""); !errors.Is(err, ErrForbidden) {
		t.Fatal("revocation not immediate")
	}
	if changed, err := s.Revoke(ctx, admin, target, in); err != nil || changed {
		t.Fatal("duplicate revoke not idempotent")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM access_audit_events`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("audit count %d %v", count, err)
	}
}
func TestValidationAndAuditRollback(t *testing.T) {
	db, s, admin, target := fixture(t)
	ctx := context.Background()
	for _, in := range []Change{{Role: "content_editor"}, {Role: "content_editor", Scope: "bad", Reason: "Reason"}, {Role: "ministry_leader", Reason: "Reason"}, {Role: "ministry_leader", Scope: "33333333-3333-4333-8333-333333333333", Reason: "Reason"}} {
		if _, err := s.Grant(ctx, admin, target, in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid accepted: %+v %v", in, err)
		}
	}
	if _, err := db.Exec(`CREATE FUNCTION fail_access_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit failed'; END $$; CREATE TRIGGER fail_access_audit BEFORE INSERT ON access_audit_events FOR EACH ROW EXECUTE FUNCTION fail_access_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant(ctx, admin, target, Change{Role: "content_editor", Reason: "Test rollback"}); err == nil {
		t.Fatal("unaudited grant")
	}
	if err := s.Check(ctx, target, "sermons.manage", ""); !errors.Is(err, ErrForbidden) {
		t.Fatal("failed grant persisted")
	}
}
func TestConcurrentGrantsAndRevokedAdministrator(t *testing.T) {
	db, s, admin, target := fixture(t)
	ctx := context.Background()
	in := Change{Role: "content_editor", Reason: "Appointment"}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, err := s.Grant(ctx, admin, target, in)
			results <- changed
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	count := 0
	for changed := range results {
		if changed {
			count++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatal("duplicate grants")
	}
	if _, err := db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke(ctx, admin, target, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator: %v", err)
	}
}

func TestUnverifiedAccountsAndRevocationRollback(t *testing.T) {
	db, s, admin, target := fixture(t)
	ctx := context.Background()
	in := Change{Role: "content_editor", Reason: "Appointment"}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant(ctx, admin, target, in); !errors.Is(err, ErrInvalid) {
		t.Fatal("unverified target granted")
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=now() WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant(ctx, admin, target, in); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION fail_revoke_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit failed'; END $$; CREATE TRIGGER fail_revoke_audit BEFORE INSERT ON access_audit_events FOR EACH ROW EXECUTE FUNCTION fail_revoke_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke(ctx, admin, target, in); err == nil {
		t.Fatal("unaudited revoke")
	}
	if err := s.Check(ctx, target, "sermons.manage", ""); err != nil {
		t.Fatal("failed revoke persisted")
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke(ctx, admin, target, in); !errors.Is(err, ErrForbidden) {
		t.Fatal("unverified admin delegated")
	}
}
func TestPendingAdministratorRevocationBlocksDelegation(t *testing.T) {
	db, s, admin, target := fixture(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM account_roles WHERE account_id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err = s.Grant(bounded, admin, target, Change{Role: "content_editor", Reason: "Must not pass"}); err == nil {
		t.Fatal("pending revocation bypassed")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, target, "sermons.manage", ""); !errors.Is(err, ErrForbidden) {
		t.Fatal("grant persisted")
	}
}

func TestUpgradePreservesLegacyEditorialRoles(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../database/migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	editor := account(t, db)
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'content_editor')`, editor); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = NewService(db).Check(ctx, editor, "sermons.manage", ""); err != nil {
		t.Fatalf("legacy editor lost access: %v", err)
	}
	var scope string
	if err = db.QueryRow(`SELECT scope FROM account_roles WHERE account_id=$1`, editor).Scan(&scope); err != nil || scope != "" {
		t.Fatal("legacy scope changed")
	}
	var admins int
	if err = db.QueryRow(`SELECT count(*) FROM account_roles WHERE role='access_administrator'`).Scan(&admins); err != nil || admins != 0 {
		t.Fatal("migration promoted an account")
	}
}

func TestDelegationDoesNotRequireExclusiveAccountLocks(t *testing.T) {
	db, s, admin, other := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'access_administrator')`, other); err != nil {
		t.Fatal(err)
	}
	reader, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	rows, err := reader.QueryContext(ctx, `SELECT id FROM accounts WHERE id IN ($1,$2) ORDER BY id FOR SHARE`, admin, other)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	errs := make(chan error, 2)
	for _, pair := range [][2]string{{admin, other}, {other, admin}} {
		go func(actor, target string) {
			_, err := s.Grant(bounded, actor, target, Change{Role: "content_editor", Reason: "Independent editorial appointment"})
			errs <- err
		}(pair[0], pair[1])
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("delegation blocked by ordinary account readers: %v", err)
		}
	}
}

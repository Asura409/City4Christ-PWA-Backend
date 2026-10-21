package database

import (
	"context"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pressly/goose/v3"
	"io/fs"
	"testing"
)

func TestChurchRoleMigrationPreservesAndDoesNotBootstrap(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('church-upgrade@example.invalid','Existing','Editor',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'content_editor')`, id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var roles, bootstrap, enabled int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM account_roles),(SELECT count(*) FROM church_role_bootstrap),(SELECT count(*) FROM role_definitions WHERE assignment_enabled AND delegation_policy='protected_church')`).Scan(&roles, &bootstrap, &enabled); err != nil {
		t.Fatal(err)
	}
	if roles != 1 || bootstrap != 0 || enabled != 4 {
		t.Fatalf("upgrade roles=%d bootstrap=%d protected=%d", roles, bootstrap, enabled)
	}
	if _, err = db.Exec(`INSERT INTO access_scopes(id,kind) VALUES('11111111-1111-4111-8111-111111111111','ministry')`); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"super_administrator", "assistant_pastor", "staff", "finance_officer"} {
		if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, id, role); err != nil {
			t.Fatal(role, err)
		}
		if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,'11111111-1111-4111-8111-111111111111')`, id, role); err == nil {
			t.Fatal("scoped church role accepted")
		}
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'unknown')`, id); err == nil {
		t.Fatal("unknown role accepted")
	}
	if _, err = db.Exec(`INSERT INTO access_audit_events(actor_id,target_id,role,scope,action,reason) VALUES($1,$1,'super_administrator','','revoked','Approved self resignation')`, id); err != nil {
		t.Fatal("self resignation audit rejected", err)
	}
	for _, change := range [][2]string{{"super_administrator", "granted"}, {"staff", "revoked"}} {
		if _, err = db.Exec(`INSERT INTO access_audit_events(actor_id,target_id,role,scope,action,reason) VALUES($1,$1,$2,'',$3,'Invalid self change')`, id, change[0], change[1]); err == nil {
			t.Fatal("invalid self audit accepted", change)
		}
	}
	if _, err = db.Exec(`INSERT INTO church_role_bootstrap(id,account_id,reason) VALUES(true,$1,'Approved operator appointment')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO church_role_bootstrap(id,account_id,reason) VALUES(false,$1,'Bypass singleton')`, id); err == nil {
		t.Fatal("false singleton accepted")
	}
	if _, err = db.Exec(`INSERT INTO church_role_bootstrap(id,account_id,reason) VALUES(true,$1,'Second bootstrap')`, id); err == nil {
		t.Fatal("second bootstrap accepted")
	}
}

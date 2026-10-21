package database

import (
	"context"
	"io/fs"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pressly/goose/v3"
)

func TestRoleCatalogUpgradePreservesAssignments(t *testing.T) {
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
	if _, err = provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('catalog@example.invalid','Existing','Admin',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'access_administrator'),($1,'system_administrator')`, id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var total, enabled, assignments int
	if err = db.QueryRow(`SELECT count(*),count(*) FILTER(WHERE assignment_enabled) FROM role_definitions`).Scan(&total, &enabled); err != nil {
		t.Fatal(err)
	}
	if total != 8 || enabled != 4 {
		t.Fatalf("catalog total=%d active=%d", total, enabled)
	}
	if err = db.QueryRow(`SELECT count(*) FROM account_roles WHERE account_id=$1 AND role IN ('access_administrator','system_administrator')`, id).Scan(&assignments); err != nil || assignments != 2 {
		t.Fatal("existing assignments changed", assignments, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM account_roles`).Scan(&assignments); err != nil || assignments != 2 {
		t.Fatal("migration assigned roles", assignments, err)
	}
	for _, role := range []string{"super_administrator", "assistant_pastor", "staff", "finance_officer", "unknown"} {
		if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, id, role); err == nil {
			t.Fatalf("reserved/unknown role assigned: %s", role)
		}
	}
	if _, err = db.Exec(`DELETE FROM role_definitions WHERE role='access_administrator'`); err == nil {
		t.Fatal("referenced role definition deleted")
	}
	if _, err = db.Exec(`UPDATE role_definitions SET scope_kind='arbitrary' WHERE role='content_editor'`); err == nil {
		t.Fatal("invalid scope kind accepted")
	}
}

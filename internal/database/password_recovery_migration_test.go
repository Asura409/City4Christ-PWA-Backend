package database

import (
	"context"
	"io/fs"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pressly/goose/v3"
)

func TestPasswordRecoveryUpgradePreservesExistingData(t *testing.T) {
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
	if _, err = provider.UpTo(ctx, 12); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name) VALUES('preserve@example.invalid','Existing','Member') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var email string
	var visible bool
	if err = db.QueryRow("SELECT email,directory_visible FROM accounts WHERE id=$1", id).Scan(&email, &visible); err != nil || email != "preserve@example.invalid" || visible {
		t.Fatal("existing account changed", err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM password_recoveries").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration seeded recovery data", err)
	}
}

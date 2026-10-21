package database

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pressly/goose/v3"
)

func TestReauthenticationUpgradeDoesNotInventAuthenticationProof(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.UpTo(ctx, 14); err != nil {
		t.Fatal(err)
	}
	var account, sid string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('upgrade-reauth@example.invalid','Existing','Session',now()) RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO auth_sessions(id,account_id,expires_at) VALUES(gen_random_uuid(),$1,now()+interval '1 day') RETURNING id`, account).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var original, confirmed *time.Time
	var audits int
	if err = db.QueryRow(`SELECT authenticated_at,reauthenticated_at FROM auth_sessions WHERE id=$1`, sid).Scan(&original, &confirmed); err != nil || original != nil || confirmed != nil {
		t.Fatal("migration invented freshness or removed session", err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM reauthentication_audit_events`).Scan(&audits); err != nil || audits != 0 {
		t.Fatal("migration seeded confirmations", err)
	}
	if _, err = db.Exec(`UPDATE auth_sessions SET reauthenticated_at=now() WHERE id=$1`, sid); err == nil {
		t.Fatal("unpaired confirmation accepted")
	}
}

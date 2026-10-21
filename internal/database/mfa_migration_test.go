package database

import (
	"context"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pressly/goose/v3"
	"io/fs"
	"testing"
)

func TestMFAUpgradePreservesSessionsAndDoesNotEnrollAccounts(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.UpTo(ctx, 15); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('mfa-upgrade@example.invalid','Existing','Member',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO auth_sessions(id,account_id,expires_at,authenticated_at) VALUES(gen_random_uuid(),$1,now()+interval '1 day',now())`, id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var sessions, factors, proofs, codes, audits int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM auth_sessions WHERE account_id=$1 AND authenticated_at IS NOT NULL),(SELECT count(*) FROM mfa_factors),(SELECT count(*) FROM mfa_session_proofs),(SELECT count(*) FROM mfa_recovery_codes),(SELECT count(*) FROM mfa_audit_events)`, id).Scan(&sessions, &factors, &proofs, &codes, &audits); err != nil || sessions != 1 || factors+proofs+codes+audits != 0 {
		t.Fatal("unsafe upgrade", sessions, factors, proofs, codes, audits, err)
	}
}

package main

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pquerna/otp/totp"
	"time"
)

func TestBootstrapOptionsFailClosedBeforeConfiguration(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	valid := []string{"--account-id", id, "--confirm-account-id", id, "--reason", "Approved appointment"}
	opts, err := parseBootstrapOptions(valid)
	if err != nil || opts.accountID != id {
		t.Fatal(opts, err)
	}
	for _, args := range [][]string{nil, {"--account-id", id, "--reason", "Approved"}, {"--account-id", id, "--confirm-account-id", "22222222-2222-4222-8222-222222222222", "--reason", "Approved"}, {"--account-id", "00000000-0000-0000-0000-000000000000", "--confirm-account-id", "00000000-0000-0000-0000-000000000000", "--reason", "Approved"}, append(append([]string{}, valid...), "extra"), append(append([]string{}, valid...), "--unknown", "secret-marker"), {"--account-id", id, "--confirm-account-id", id, "--reason", "  "}} {
		if _, err = parseBootstrapOptions(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
		err = run(context.Background(), append([]string{"bootstrap-super-admin"}, args...))
		if err == nil || strings.Contains(err.Error(), "DATABASE_URL") || strings.Contains(err.Error(), "secret-marker") {
			t.Fatalf("unsafe/pre-config failure: %v", err)
		}
	}
}

func TestBootstrapCommandUsesDisposableDatabaseAndIsOneTime(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var socket, user, name, id string
	if err := db.QueryRow(`SELECT current_setting('unix_socket_directories'),current_user,current_database()`).Scan(&socket, &user, &name); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	t.Setenv("DATABASE_URL", "postgres://"+user+"@/"+name+"?host="+socket+"&sslmode=disable")
	t.Setenv("VERIFICATION_KEY_BASE64", base64.StdEncoding.EncodeToString(key))
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('bootstrap-cli@example.invalid','CLI','Tester',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	args := []string{"bootstrap-super-admin", "--account-id", id, "--confirm-account-id", id, "--reason", "Owner-approved disposable test"}
	if err := run(ctx, args); err == nil {
		t.Fatal("unenrolled account bootstrapped")
	}
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mfa, err := auth.NewMFAService(db, sessions, key)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := mfa.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mfa.Confirm(ctx, tokens.AccessToken, setup.ID, code); err != nil {
		t.Fatal(err)
	}
	if err = run(ctx, args); err != nil {
		t.Fatal(err)
	}
	if err = run(ctx, args); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("repeated bootstrap: %v", err)
	}
	var assignments, audits int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM account_roles WHERE role='super_administrator'),(SELECT count(*) FROM church_role_bootstrap)`).Scan(&assignments, &audits); err != nil || assignments != 1 || audits != 1 {
		t.Fatal(assignments, audits, err)
	}
}

func TestBootstrapCommandDoesNotApplyMigrations(t *testing.T) {
	db := testpg.New(t)
	var socket, user, name string
	if err := db.QueryRow(`SELECT current_setting('unix_socket_directories'),current_user,current_database()`).Scan(&socket, &user, &name); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://"+user+"@/"+name+"?host="+socket+"&sslmode=disable")
	t.Setenv("VERIFICATION_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	id := "11111111-1111-4111-8111-111111111111"
	err := run(context.Background(), []string{"bootstrap-super-admin", "--account-id", id, "--confirm-account-id", id, "--reason", "Approved"})
	if err == nil || strings.Contains(err.Error(), socket) {
		t.Fatal("unsafe database error", err)
	}
	var absent bool
	if err = db.QueryRow(`SELECT to_regclass('accounts') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("bootstrap applied migrations", err)
	}
}

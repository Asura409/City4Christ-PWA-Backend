package profiles

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func fixture(t *testing.T) (*sql.DB, *Repository, string, string) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	create := func(email string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,password_hash) VALUES($1,'Original','Member',now(),'test-hash') RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	return db, NewRepository(db), create("first@example.invalid"), create("second@example.invalid")
}
func TestRepositoryProfileOwnershipAndConcurrentPartialEdits(t *testing.T) {
	db, r, first, second := fixture(t)
	ctx := context.Background()
	p, err := r.Get(ctx, first)
	if err != nil || p.DirectoryVisible {
		t.Fatalf("privacy default: %+v %v", p, err)
	}
	name, last, visible := "Updated", "Surname", true
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, in := range []Update{{FirstName: &name}, {LastName: &last, DirectoryVisible: &visible}} {
		wg.Add(1)
		go func(in Update) { defer wg.Done(); _, err := r.Update(ctx, first, in); errs <- err }(in)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err = r.Get(ctx, first)
	if err != nil || p.FirstName != name || p.LastName != last || !p.DirectoryVisible || p.Email != "first@example.invalid" || !p.HasPassword || !p.EmailVerified {
		t.Fatalf("partial edit: %+v %v", p, err)
	}
	other, err := r.Get(ctx, second)
	if err != nil || other.FirstName != "Original" || other.DirectoryVisible {
		t.Fatal("another account changed")
	}
	var hash string
	if err = db.QueryRow(`SELECT password_hash FROM accounts WHERE id=$1`, first).Scan(&hash); err != nil || hash != "test-hash" {
		t.Fatal("security fields changed")
	}
	if _, err = r.Get(ctx, "33333333-3333-4333-8333-333333333333"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing profile not handled")
	}
}
func TestServiceValidationAndPrivacyToggle(t *testing.T) {
	_, r, first, _ := fixture(t)
	s := NewService(r)
	ctx := context.Background()
	name := "  Grace  "
	visible := true
	p, err := s.Update(ctx, first, Update{FirstName: &name, DirectoryVisible: &visible})
	if err != nil || p.FirstName != "Grace" || !p.DirectoryVisible {
		t.Fatalf("valid update: %+v %v", p, err)
	}
	visible = false
	p, err = s.Update(ctx, first, Update{DirectoryVisible: &visible})
	if err != nil || p.DirectoryVisible || p.FirstName != "Grace" {
		t.Fatal("opt out or omitted field failed")
	}
	for _, bad := range []string{"", "  ", strings.Repeat("a", 101), "name\x00", string([]byte{255}), "bad\nname"} {
		_, err = s.Update(ctx, first, Update{FirstName: &bad})
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["firstName"] == "" {
			t.Fatalf("invalid name accepted: %v", err)
		}
	}
	if _, err = s.Update(ctx, first, Update{}); err == nil {
		t.Fatal("empty patch accepted")
	}
	p, err = s.Get(ctx, first)
	if err != nil || p.FirstName != "Grace" {
		t.Fatal("invalid edits persisted")
	}
	if _, err = s.Get(ctx, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid actor: %v", err)
	}
}

func TestMigrationPreservesExistingProfilesAndDefaultsPrivate(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../database/migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('legacy@example.invalid',' Legacy ',' Member ',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'content_editor')`, id); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := NewService(NewRepository(db))
	p, err := s.Get(ctx, id)
	if err != nil || p.DirectoryVisible || p.FirstName != " Legacy " {
		t.Fatalf("legacy privacy: %+v %v", p, err)
	}
	visible := true
	p, err = s.Update(ctx, id, Update{DirectoryVisible: &visible})
	if err != nil || !p.DirectoryVisible || p.FirstName != " Legacy " || p.LastName != " Member " {
		t.Fatal("privacy change rewrote legacy names")
	}
	var roles int
	if err = db.QueryRow(`SELECT count(*) FROM account_roles WHERE account_id=$1 AND role='content_editor'`, id).Scan(&roles); err != nil || roles != 1 {
		t.Fatal("migration altered existing access")
	}
}

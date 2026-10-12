package auth

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestAccountRepository(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Reapplying migrations must preserve existing records.
	repo := NewAccountRepository(db)
	hash, err := NewPasswordHasher().Hash("a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	account, err := repo.Create(ctx, NewAccount{Email: "  Member@Example.COM  ", FirstName: "Ada", LastName: "Member", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if account.ID == "" || account.Email != "member@example.com" || account.EmailVerifiedAt != nil || account.CreatedAt.IsZero() {
		t.Fatalf("unexpected account: %+v", account)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ByEmail(ctx, " MEMBER@example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != account.ID || got.FirstName != "Ada" || got.LastName != "Member" || got.PasswordHash == nil || *got.PasswordHash != hash {
		t.Fatal("account did not round trip")
	}
	if _, err := repo.ByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("missing account: %v", err)
	}
	// Uniqueness must hold under concurrent registration attempts.
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Create(ctx, NewAccount{Email: "MEMBER@example.com", FirstName: "Other", LastName: "Member", PasswordHash: &hash})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if !errors.Is(err, ErrEmailTaken) {
			t.Fatalf("duplicate: %v", err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM accounts").Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	// Database itself must reject non-normalized and blank identities.
	for _, email := range []string{"Member@example.com", " member@example.com ", ""} {
		if _, err := db.ExecContext(ctx, "INSERT INTO accounts(email,first_name,last_name) VALUES ($1,'A','B')", email); err == nil {
			t.Fatalf("database accepted invalid email %q", email)
		}
	}
	// OIDC-only accounts have no password by default.
	google, err := repo.Create(ctx, NewAccount{Email: "google@example.com", FirstName: "Google", LastName: "Member"})
	if err != nil {
		t.Fatal(err)
	}
	if google.PasswordHash != nil {
		t.Fatal("OIDC account must not receive a local password")
	}
	loaded, err := repo.ByEmail(ctx, google.Email)
	if err != nil || loaded.PasswordHash != nil {
		t.Fatalf("nullable password round trip: %v", err)
	}
}

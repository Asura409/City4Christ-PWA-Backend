package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestRegistrationService(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := NewAccountRepository(db)
	verification, err := NewVerificationService(db, VerificationConfig{Key: bytes.Repeat([]byte{42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	service := NewRegistrationService(NewPasswordHasher(), verification)
	valid := Registration{FirstName: "Ada", LastName: "Member", Email: " ADA@EXAMPLE.COM ", Password: "a sufficiently long password"}
	account, err := service.Register(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	if account.Email != "ada@example.com" || account.EmailVerifiedAt != nil {
		t.Fatalf("unexpected account: %+v", account)
	}
	stored, err := repo.ByEmail(ctx, valid.Email)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswordHash == nil || *stored.PasswordHash == valid.Password || !NewPasswordHasher().Verify(valid.Password, *stored.PasswordHash) {
		t.Fatal("password not securely persisted")
	}
	if _, err := service.Register(ctx, valid); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate error: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Registration)
		field  string
	}{
		{"invalid email", func(r *Registration) { r.Email = "not-an-email" }, "email"},
		{"display name email", func(r *Registration) { r.Email = "Ada <ada@example.com>" }, "email"},
		{"missing first name", func(r *Registration) { r.FirstName = "  " }, "firstName"},
		{"missing last name", func(r *Registration) { r.LastName = "" }, "lastName"},
		{"oversized name", func(r *Registration) { r.FirstName = strings.Repeat("A", 101) }, "firstName"},
		{"short password", func(r *Registration) { r.Password = "too short" }, "password"},
		{"oversized password", func(r *Registration) { r.Password = strings.Repeat("a", 1025) }, "password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.change(&input)
			_, err := service.Register(ctx, input)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Fields[tc.field] == "" {
				t.Fatalf("expected %s validation, got %v", tc.field, err)
			}
		})
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM accounts").Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid input persisted: count=%d err=%v", count, err)
	}
}

package auth

import (
	"context"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type Registration struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Password  string `json:"password"`
}

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "invalid registration" }

type RegistrationService struct {
	passwords    PasswordHasher
	verification *VerificationService
}

func NewRegistrationService(passwords PasswordHasher, verification *VerificationService) *RegistrationService {
	return &RegistrationService{passwords: passwords, verification: verification}
}

// Register persists an unverified account. It does not grant member access.
func (s *RegistrationService) Register(ctx context.Context, input Registration) (Account, error) {
	input.Email = normalizeEmail(input.Email)
	input.FirstName = strings.TrimSpace(input.FirstName)
	input.LastName = strings.TrimSpace(input.LastName)
	fields := make(map[string]string)
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || !strings.Contains(input.Email, "@") || len(input.Email) > 254 {
		fields["email"] = "Enter a valid email address."
	}
	for field, value := range map[string]string{"firstName": input.FirstName, "lastName": input.LastName} {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 100 {
			fields[field] = "Use between 1 and 100 characters."
		}
	}
	// Bound hashing work and request size; preserve the password exactly as supplied.
	if !utf8.ValidString(input.Password) || utf8.RuneCountInString(input.Password) < 15 || len(input.Password) > 1024 {
		fields["password"] = "Use at least 15 characters and at most 1024 bytes."
	}
	if len(fields) > 0 {
		return Account{}, &ValidationError{Fields: fields}
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return Account{}, err
	}
	if s.verification == nil {
		return Account{}, ErrVerificationUnavailable
	}
	return s.verification.register(ctx, NewAccount{Email: input.Email, FirstName: input.FirstName, LastName: input.LastName, PasswordHash: &hash})
}

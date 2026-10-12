package auth

import "github.com/alexedwards/argon2id"

// PasswordHasher uses the maintained Argon2id library for hashing and comparison.
// Its parameters are fixed here; callers cannot weaken them.
type PasswordHasher struct{}

func NewPasswordHasher() PasswordHasher { return PasswordHasher{} }

func (PasswordHasher) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, &argon2id.Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	})
}

// Verify fails closed for absent or invalid stored credentials.
func (PasswordHasher) Verify(password, hash string) bool {
	if hash == "" {
		return false
	}
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && match
}

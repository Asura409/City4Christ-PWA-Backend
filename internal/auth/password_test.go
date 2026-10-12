package auth

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	h := NewPasswordHasher()
	password := "a sufficiently long password 🔐"
	first, err := h.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected parameters: %s", first)
	}
	if first == second {
		t.Fatal("hashes must use independent random salts")
	}
	for _, tc := range []struct {
		name, password, hash string
		want                 bool
	}{
		{"correct", password, first, true},
		{"incorrect", "a different long password", first, false},
		{"no local password", password, "", false},
		{"malformed", password, "not-a-hash", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.Verify(tc.password, tc.hash); got != tc.want {
				t.Fatalf("Verify = %v, want %v", got, tc.want)
			}
		})
	}
}

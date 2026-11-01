package visitors

import (
	"strings"
	"testing"
)

func TestVisitorCiphertextIsBoundToRecordAndRotationKeepsOldReads(t *testing.T) {
	old := []byte(strings.Repeat("a", 32))
	p, e := NewProtection("old", map[string][]byte{"old": old})
	if e != nil {
		t.Fatal(e)
	}
	in := validSubmission()
	sealed, e := p.seal("first", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.open("second", "old", sealed); e == nil {
		t.Fatal("ciphertext moved to another record")
	}
	sealed[0] ^= 1
	if _, e = p.open("first", "old", sealed); e == nil {
		t.Fatal("tampered ciphertext")
	}
	sealed, e = p.seal("first", in)
	if e != nil {
		t.Fatal(e)
	}
	rotated, e := NewProtection("new", map[string][]byte{"old": old, "new": []byte(strings.Repeat("b", 32))})
	if e != nil {
		t.Fatal(e)
	}
	if c, e := rotated.open("first", "old", sealed); e != nil || c.Email != in.Email {
		t.Fatal("rotation read", e)
	}
	if _, e = rotated.open("first", "missing", sealed); e == nil {
		t.Fatal("unknown key")
	}
}

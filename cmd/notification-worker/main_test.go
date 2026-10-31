package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyFileIsPrivateAndNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push.env")
	if e := generateKeys(path); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	body, e := os.ReadFile(path)
	if e != nil || !strings.Contains(string(body), "PUSH_VAPID_PRIVATE_KEY=") || !strings.Contains(string(body), "PUSH_VAPID_PUBLIC_KEY=") {
		t.Fatal("missing key settings")
	}
	if e = generateKeys(path); e == nil {
		t.Fatal("overwrote keys")
	}
}

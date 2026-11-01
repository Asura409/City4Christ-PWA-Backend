package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisitorConfigurationAndPrivateKeyFile(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://test:test@localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString(make([]byte, 32)), "VISITOR_INTAKE_ENABLED": "true"}
	if _, e := loadConfig(func(k string) string { return values[k] }); e == nil {
		t.Fatal("enabled intake without a dedicated key")
	}
	file := filepath.Join(t.TempDir(), "visitor.env")
	if e := generateVisitorKeys(file); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(file)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	if e = generateVisitorKeys(file); e == nil {
		t.Fatal("overwrote existing key file")
	}
}

func TestGeneratedVisitorSettingsLoadWithoutShellQuoting(t *testing.T) {
	file := filepath.Join(t.TempDir(), "visitor.env")
	if e := generateVisitorKeys(file); e != nil {
		t.Fatal(e)
	}
	body, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	if values["VISITOR_DATA_KEY_BASE64"] == "" {
		t.Fatal("missing simple current key setting")
	}
	values["VISITOR_INTAKE_ENABLED"] = "true"
	enabled, p, e := loadVisitorConfig(func(k string) string { return values[k] })
	if e != nil || !enabled || p == nil {
		t.Fatal("generated settings cannot be loaded directly", e)
	}
}

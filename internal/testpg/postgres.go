// Package testpg starts an isolated PostgreSQL cluster for integration tests.
// It never reads DATABASE_URL or connects to an existing server.
package testpg

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func New(t *testing.T) *sql.DB {
	t.Helper()
	bin := os.Getenv("TEST_POSTGRES_BIN")
	if bin == "" {
		if p, err := exec.LookPath("initdb"); err == nil {
			bin = filepath.Dir(p)
		} else {
			matches, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb")
			if len(matches) == 0 {
				t.Fatal("PostgreSQL binaries required; set TEST_POSTGRES_BIN")
			}
			bin = filepath.Dir(matches[len(matches)-1])
		}
	}
	// A short private directory avoids Unix socket path length limits.
	root, err := os.MkdirTemp("", "c4c-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	run := func(name string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, filepath.Join(bin, name), args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
	}
	data := filepath.Join(root, "data")
	run("initdb", "-D", data, "-U", "c4c_test", "--auth-local=trust", "--auth-host=reject", "--encoding=UTF8", "--no-locale", "--no-instructions")
	// TCP is disabled. Only the owner can access this cluster's socket directory.
	run("pg_ctl", "-D", data, "-l", filepath.Join(root, "postgres.log"), "-o", "-k "+root+" -c listen_addresses=''", "-w", "start")
	t.Cleanup(func() { run("pg_ctl", "-D", data, "-m", "immediate", "-w", "stop") })
	db, err := sql.Open("pgx", "host="+root+" user=c4c_test dbname=postgres sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE c4c_test"); err != nil {
		t.Fatal(err)
	}
	testDB, err := sql.Open("pgx", "host="+root+" user=c4c_test dbname=c4c_test sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testDB.Close() })
	if err := testDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	return testDB
}

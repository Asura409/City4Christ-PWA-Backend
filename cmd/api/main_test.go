package main

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestConfigFailsClosed(t *testing.T) {
	valid := map[string]string{"DATABASE_URL": "postgres://test:test@localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString(make([]byte, 32))}
	cfg, err := loadConfig(func(k string) string { return valid[k] })
	if err != nil || cfg.port != "10000" || cfg.registrationEnabled {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	for _, key := range []string{"DATABASE_URL", "VERIFICATION_KEY_BASE64"} {
		t.Run(key, func(t *testing.T) {
			_, err := loadConfig(func(k string) string {
				if k == key {
					return ""
				}
				return valid[k]
			})
			if err == nil {
				t.Fatal("missing config accepted")
			}
		})
	}
	for _, bad := range []map[string]string{{"PORT": "0"}, {"PORT": "65536"}, {"PORT": "oops"}, {"REGISTRATION_ENABLED": "yes"}, {"DATABASE_URL": "://bad"}} {
		_, err := loadConfig(func(k string) string {
			if v, ok := bad[k]; ok {
				return v
			}
			return valid[k]
		})
		if err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestStartupMigrationPreservesDataAndServesGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (email,first_name,last_name) VALUES ('preserve@example.org','Test','User') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("data lost: %d %v", count, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- serve(serverCtx, db, listener, serverConfig{verificationKey: make([]byte, 32)}) }()
	client := &http.Client{Timeout: 3 * time.Second}
	for _, path := range []string{"/health/live", "/health/ready"} {
		resp, err := client.Get("http://" + listener.Addr().String() + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("X-Request-Id") == "" {
			t.Fatalf("global app health: %v", resp)
		}
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown timed out")
	}
}

package main

import (
	"context"
	"encoding/base64"
	"fmt"
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

func TestR2MediaStorageConfigIsOptionalButMustBeComplete(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":            "postgres://test:test@localhost/test",
		"VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"R2_ACCOUNT_ID":           "0123456789abcdef0123456789abcdef",
		"R2_BUCKET":               "city4christ-media-staging",
		"R2_ACCESS_KEY_ID":        "test-access",
		"R2_SECRET_ACCESS_KEY":    "test-secret",
	}
	config, err := loadConfig(func(key string) string { return values[key] })
	if err != nil || !config.r2Configured || config.r2Config.Bucket != values["R2_BUCKET"] {
		t.Fatalf("R2 config = %+v, err = %v", config, err)
	}
	for _, missing := range []string{"R2_ACCOUNT_ID", "R2_BUCKET", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY"} {
		t.Run(missing, func(t *testing.T) {
			_, err := loadConfig(func(key string) string {
				if key == missing {
					return ""
				}
				return values[key]
			})
			if err == nil {
				t.Fatalf("partial R2 config accepted without %s", missing)
			}
		})
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

func TestBrowserAndGoogleConfig(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://test@localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString(make([]byte, 32)), "APP_ORIGIN": "https://api.example.invalid", "CORS_ORIGINS": "https://zeetechdot.github.io", "REFRESH_COOKIE_SAME_SITE": "none", "TRUSTED_PROXY_CIDRS": "127.0.0.1/32"}
	c, err := loadConfig(func(k string) string { return env[k] })
	if err != nil || c.appOrigin != env["APP_ORIGIN"] || !c.crossSiteCookies || len(c.allowedOrigins) != 1 || len(c.trustedProxyCIDRs) != 1 {
		t.Fatalf("browser config: %+v %v", c, err)
	}
	for _, bad := range []map[string]string{{"APP_ORIGIN": "https://api.example.invalid/path"}, {"CORS_ORIGINS": "*"}, {"REFRESH_COOKIE_SAME_SITE": "unsafe"}, {"TRUSTED_PROXY_CIDRS": "all"}, {"GOOGLE_CLIENT_ID": "incomplete"}} {
		_, err := loadConfig(func(k string) string {
			if v, ok := bad[k]; ok {
				return v
			}
			return env[k]
		})
		if err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestMigrateAndServeStartsWithoutResettingData(t *testing.T) {
	db := testpg.New(t)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var socket, user, name string
	if err := db.QueryRow(`SELECT current_setting('unix_socket_directories'),current_user,current_database()`).Scan(&socket, &user, &name); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://"+user+"@/"+name+"?host="+socket+"&sslmode=disable")
	t.Setenv("VERIFICATION_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	t.Setenv("PORT", fmt.Sprint(port))
	if _, err := db.Exec(`INSERT INTO accounts(email,first_name,last_name) VALUES ('free@example.invalid','Free','Test')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"migrate-and-serve"}) }()
	ready := false
	for i := 0; i < 100; i++ {
		select {
		case err := <-done:
			t.Fatalf("startup failed: %v", err)
		default:
		}
		response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health/ready", port))
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not become ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM accounts WHERE email='free@example.invalid'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("existing data lost: %d %v", count, err)
	}
}

func TestPushAPIConfigFailsClosed(t *testing.T) {
	valid := map[string]string{"DATABASE_URL": "postgres://test:test@localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString(make([]byte, 32))}
	for _, setting := range []string{"yes", "true"} {
		valid["PUSH_ENABLED"] = setting
		if _, err := loadConfig(func(k string) string { return valid[k] }); err == nil {
			t.Fatal("invalid/incomplete push configuration accepted", setting)
		}
	}
}

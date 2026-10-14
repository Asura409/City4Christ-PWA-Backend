// auth-preview is a temporary test-only host. It never sends email or migrates data.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/app"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed index.html
var page []byte

//go:embed preview.js
var script []byte

type config struct {
	origin, password, databaseURL, port string
	key                                 []byte
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{origin: getenv("APP_ORIGIN"), password: getenv("PREVIEW_PASSWORD"), databaseURL: getenv("DATABASE_URL"), port: getenv("PORT")}
	u, err := url.Parse(c.origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("APP_ORIGIN must be an HTTPS origin without path or credentials")
	}
	if len(c.password) < 24 {
		return c, errors.New("PREVIEW_PASSWORD must have at least 24 characters")
	}
	if c.databaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if _, err := pgx.ParseConfig(c.databaseURL); err != nil {
		return c, errors.New("DATABASE_URL is invalid")
	}
	c.key, err = base64.StdEncoding.DecodeString(getenv("VERIFICATION_KEY_BASE64"))
	if err != nil || len(c.key) != 32 {
		return c, errors.New("verification key must encode 32 bytes")
	}
	if c.port == "" {
		c.port = "10000"
	}
	p, err := strconv.Atoi(c.port)
	if err != nil || p < 1 || p > 65535 {
		return c, errors.New("PORT is invalid")
	}
	return c, nil
}

func handler(db *sql.DB, c config) http.Handler {
	global := app.New(db, app.Config{RegistrationEnabled: true, VerificationKey: c.key, AppOrigin: c.origin})
	verification, err := auth.NewVerificationService(db, auth.VerificationConfig{Key: c.key})
	if err != nil {
		panic("invalid preview key")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/auth-test", http.StatusSeeOther) })
	mux.HandleFunc("GET /auth-test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /auth-test/preview.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(script)
	})
	mux.HandleFunc("GET /auth-test/inbox", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var accountID, challengeID string
		var sealed []byte
		email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		err := db.QueryRowContext(ctx, `SELECT o.account_id,o.challenge_id,o.sealed_payload FROM verification_email_outbox o JOIN accounts a ON a.id=o.account_id WHERE a.email=$1`, email).Scan(&accountID, &challengeID, &sealed)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "No queued test email", 404)
			return
		}
		if err != nil {
			http.Error(w, "Inbox unavailable", 503)
			return
		}
		message, err := verification.OpenEmail(accountID, challengeID, sealed)
		if err != nil {
			http.Error(w, "Inbox unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message)
	})
	mux.Handle("/", global)
	expected := sha256.Sum256([]byte(c.password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		username, password, ok := r.BasicAuth()
		provided := sha256.Sum256([]byte(password))
		bearerRoute := (r.URL.Path == "/api/v1/me" || r.URL.Path == "/api/v1/auth/google/link") && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearerRoute && (!ok || username != "preview" || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1) {
			w.Header().Set("WWW-Authenticate", `Basic realm="City4Christ test preview", charset="UTF-8"`)
			http.Error(w, "Preview authentication required", 401)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != c.origin {
			http.Error(w, "Origin not allowed", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func run(ctx context.Context, c config) error {
	db, err := sql.Open("pgx", c.databaseURL)
	if err != nil {
		return errors.New("database configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	ping, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = db.PingContext(ping)
	cancel()
	if err != nil {
		return errors.New("preview database unavailable")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", c.port))
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler(db, c), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(stop); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	slog.Info("Test-only API preview listening", "address", listener.Addr().String(), "origin", c.origin)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func main() {
	c, err := loadConfig(os.Getenv)
	if err != nil {
		slog.Error("Preview configuration failed", "reason", err.Error())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, c); err != nil {
		slog.Error("Preview stopped", "reason", err.Error())
		os.Exit(1)
	}
}

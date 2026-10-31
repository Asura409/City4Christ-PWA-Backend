// api serves HTTP, or applies forward migrations with the migrate subcommand.
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/app"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/googleoidc"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type serverConfig struct {
	pushEnabled                        bool
	pushPublicKey                      string
	databaseURL                        string
	port                               string
	verificationKey                    []byte
	registrationEnabled                bool
	appOrigin                          string
	allowedOrigins                     []string
	crossSiteCookies                   bool
	trustedProxyCIDRs                  []string
	googleClientID, googleClientSecret string
	r2Config                           media.R2Config
	r2Configured                       bool
}

func loadConfig(getenv func(string) string) (serverConfig, error) {
	c := serverConfig{databaseURL: getenv("DATABASE_URL"), port: getenv("PORT")}
	switch getenv("PUSH_ENABLED") {
	case "", "false":
	case "true":
		c.pushEnabled = true
		c.pushPublicKey = getenv("PUSH_VAPID_PUBLIC_KEY")
		if !notifications.ValidPushPublicKey(c.pushPublicKey) {
			return c, errors.New("PUSH_VAPID_PUBLIC_KEY is required when push is enabled")
		}
	default:
		return c, errors.New("PUSH_ENABLED must be true or false")
	}

	if c.databaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if _, err := pgx.ParseConfig(c.databaseURL); err != nil {
		return c, errors.New("DATABASE_URL is invalid")
	}
	key, err := base64.StdEncoding.DecodeString(getenv("VERIFICATION_KEY_BASE64"))
	if err != nil || len(key) != 32 {
		return c, errors.New("VERIFICATION_KEY_BASE64 must encode 32 bytes")
	}
	c.verificationKey = key
	if c.port == "" {
		c.port = "10000"
	}
	port, err := strconv.Atoi(c.port)
	if err != nil || port < 1 || port > 65535 {
		return c, errors.New("PORT must be between 1 and 65535")
	}
	switch getenv("REGISTRATION_ENABLED") {
	case "", "false":
	case "true":
		c.registrationEnabled = true
	default:
		return c, errors.New("REGISTRATION_ENABLED must be true or false")
	}
	c.appOrigin = getenv("APP_ORIGIN")
	validOrigin := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
	}
	if c.appOrigin != "" && !validOrigin(c.appOrigin) {
		return c, errors.New("APP_ORIGIN must be an HTTPS origin")
	}
	for _, entry := range strings.Split(getenv("CORS_ORIGINS"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !validOrigin(entry) {
			return c, errors.New("CORS_ORIGINS contains an invalid origin")
		}
		c.allowedOrigins = append(c.allowedOrigins, entry)
	}
	switch getenv("REFRESH_COOKIE_SAME_SITE") {
	case "", "lax":
	case "none":
		c.crossSiteCookies = true
	default:
		return c, errors.New("REFRESH_COOKIE_SAME_SITE must be lax or none")
	}
	for _, entry := range strings.Split(getenv("TRUSTED_PROXY_CIDRS"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, err := netip.ParsePrefix(entry); err != nil {
			return c, errors.New("TRUSTED_PROXY_CIDRS contains an invalid prefix")
		}
		c.trustedProxyCIDRs = append(c.trustedProxyCIDRs, entry)
	}
	c.googleClientID = getenv("GOOGLE_CLIENT_ID")
	c.googleClientSecret = getenv("GOOGLE_CLIENT_SECRET")
	if (c.googleClientID == "") != (c.googleClientSecret == "") || c.googleClientID != "" && c.appOrigin == "" {
		return c, errors.New("Google requires client ID, client secret, and APP_ORIGIN")
	}
	r2Values := []string{getenv("R2_ACCOUNT_ID"), getenv("R2_BUCKET"), getenv("R2_ACCESS_KEY_ID"), getenv("R2_SECRET_ACCESS_KEY")}
	configured := 0
	for _, value := range r2Values {
		if value != "" {
			configured++
		}
	}
	if configured != 0 && configured != len(r2Values) {
		return c, errors.New("R2_ACCOUNT_ID, R2_BUCKET, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY must be configured together")
	}
	if configured == len(r2Values) {
		c.r2Config = media.R2Config{AccountID: r2Values[0], Bucket: r2Values[1], AccessKeyID: r2Values[2], SecretAccessKey: r2Values[3]}
		if _, err := media.NewR2Storage(c.r2Config, nil); err != nil {
			return serverConfig{}, errors.New("R2 media storage configuration is invalid")
		}
		c.r2Configured = true
	}
	return c, nil
}

func migrate(ctx context.Context, db *sql.DB) error { return database.Migrate(ctx, db) }

func serve(ctx context.Context, db *sql.DB, listener net.Listener, c serverConfig) error {
	appConfig := app.Config{PushEnabled: c.pushEnabled, PushPublicKey: c.pushPublicKey, RegistrationEnabled: c.registrationEnabled, VerificationKey: c.verificationKey, AppOrigin: c.appOrigin, AllowedOrigins: c.allowedOrigins, CrossSiteCookies: c.crossSiteCookies, TrustedProxyCIDRs: c.trustedProxyCIDRs}
	if c.r2Configured {
		store, err := media.NewR2Storage(c.r2Config, nil)
		if err != nil {
			return errors.New("R2 media storage configuration failed")
		}
		appConfig.MediaStorage = store
	}
	if c.googleClientID != "" {
		discovery, cancel := context.WithTimeout(ctx, 15*time.Second)
		provider, err := googleoidc.New(discovery, c.googleClientID, c.googleClientSecret, c.appOrigin+"/api/v1/auth/google/callback")
		cancel()
		if err != nil {
			return err
		}
		appConfig.GoogleProvider = provider
	}
	server := &http.Server{Handler: app.New(db, appConfig), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func run(ctx context.Context, args []string) error {
	var bootstrap *bootstrapOptions
	if len(args) > 0 && args[0] == "bootstrap-super-admin" {
		options, err := parseBootstrapOptions(args[1:])
		if err != nil {
			return err
		}
		bootstrap = &options
	} else if len(args) > 1 || (len(args) == 1 && args[0] != "migrate" && args[0] != "migrate-and-serve") {
		return errors.New("usage: api [migrate|migrate-and-serve|bootstrap-super-admin --account-id UUID --confirm-account-id SAME_UUID --reason TEXT]")
	}
	c, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", c.databaseURL)
	if err != nil {
		return errors.New("database configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := db.PingContext(startup); err != nil {
		return errors.New("database connection failed")
	}
	if bootstrap != nil {
		return bootstrapSuperAdmin(startup, db, *bootstrap)
	}
	if len(args) == 1 {
		if err := migrate(startup, db); err != nil {
			return errors.New("forward migration failed")
		}
		slog.Info("Forward migrations complete")
		if args[0] == "migrate" {
			return nil
		}
	}
	listener, err := net.Listen("tcp", ":"+c.port)
	if err != nil {
		return errors.New("HTTP listener failed")
	}
	defer listener.Close()
	slog.Info("API listening", "port", c.port)
	return serve(ctx, db, listener, c)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("API stopped", "reason", err.Error())
		os.Exit(1)
	}
}

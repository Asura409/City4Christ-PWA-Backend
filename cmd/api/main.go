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
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/app"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type serverConfig struct {
	databaseURL         string
	port                string
	verificationKey     []byte
	registrationEnabled bool
}

func loadConfig(getenv func(string) string) (serverConfig, error) {
	c := serverConfig{databaseURL: getenv("DATABASE_URL"), port: getenv("PORT")}
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
	return c, nil
}

func migrate(ctx context.Context, db *sql.DB) error { return database.Migrate(ctx, db) }

func serve(ctx context.Context, db *sql.DB, listener net.Listener, c serverConfig) error {
	server := &http.Server{Handler: app.New(db, app.Config{RegistrationEnabled: c.registrationEnabled, VerificationKey: c.verificationKey}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
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
	if len(args) > 1 || (len(args) == 1 && args[0] != "migrate") {
		return errors.New("usage: api [migrate]")
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
	if len(args) == 1 {
		if err := migrate(startup, db); err != nil {
			return errors.New("forward migration failed")
		}
		slog.Info("Forward migrations complete")
		return nil
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("API stopped", "reason", err.Error())
		os.Exit(1)
	}
}

// notification-worker delivers opt-in generic Web Push previews. It never migrates
// the database and is disabled until explicitly configured.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func generateKeys(path string) error {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return errors.New("could not generate VAPID keys")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("create a new private key file; existing files are never overwritten")
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "PUSH_ENABLED=true\nPUSH_VAPID_PUBLIC_KEY=%s\nPUSH_VAPID_PRIVATE_KEY=%s\nPUSH_VAPID_SUBJECT=mailto:info@city4christ.org\n", public, private)
	return err
}
func run(ctx context.Context) error {
	cfg, err := notifications.LoadPushConfig(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		slog.Info("Web Push worker disabled")
		return nil
	}
	dsn := os.Getenv("DATABASE_URL")
	pg, err := pgx.ParseConfig(dsn)
	if err != nil || dsn == "" {
		return errors.New("DATABASE_URL is required and must be valid")
	}
	if pg.TLSConfig == nil || pg.TLSConfig.InsecureSkipVerify {
		return errors.New("notification worker requires verified PostgreSQL TLS (sslmode=verify-full)")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("database configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(3)
	ping, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = db.PingContext(ping)
	cancel()
	if err != nil {
		return errors.New("database readiness failed")
	}
	sender, err := notifications.NewVAPIDSender(cfg, nil)
	if err != nil {
		return err
	}
	worker := notifications.NewPushWorker(db, sender, nil)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	slog.Info("Web Push worker started")
	for {
		count, err := worker.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			slog.Error("Web Push batch failed; retrying")
		}
		if count > 0 {
			slog.Info("Web Push jobs processed", "count", count)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func main() {
	if len(os.Args) == 3 && os.Args[1] == "generate-keys" {
		if err := generateKeys(os.Args[2]); err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		slog.Info("Private VAPID settings saved")
		return
	}
	if len(os.Args) != 1 {
		slog.Error("Usage: notification-worker [generate-keys NEW_ENV_FILE]")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

// media-worker scans private uploads and promotes clean bytes to immutable R2 keys.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type workerConfig struct {
	enabled       bool
	databaseURL   string
	r2            media.R2Config
	clamAVBinary  string
	pollInterval  time.Duration
	leaseDuration time.Duration
}

func loadConfig(getenv func(string) string) (workerConfig, error) {
	var config workerConfig
	enabled := strings.TrimSpace(getenv("MEDIA_SCAN_ENABLED"))
	if enabled == "" || enabled == "false" {
		return config, nil
	}
	if enabled != "true" {
		return workerConfig{}, errors.New("MEDIA_SCAN_ENABLED must be true or false")
	}
	config.enabled = true
	config.databaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	if config.databaseURL == "" {
		return workerConfig{}, errors.New("DATABASE_URL is required")
	}
	if _, err := pgx.ParseConfig(config.databaseURL); err != nil {
		return workerConfig{}, errors.New("DATABASE_URL is invalid")
	}
	config.r2 = media.R2Config{
		AccountID: strings.TrimSpace(getenv("R2_ACCOUNT_ID")), Bucket: strings.TrimSpace(getenv("R2_BUCKET")),
		AccessKeyID: strings.TrimSpace(getenv("MEDIA_R2_ACCESS_KEY_ID")), SecretAccessKey: strings.TrimSpace(getenv("MEDIA_R2_SECRET_ACCESS_KEY")),
	}
	if _, err := media.NewR2Storage(config.r2, time.Now); err != nil {
		return workerConfig{}, errors.New("worker R2 credentials and bucket settings are required")
	}
	config.clamAVBinary = strings.TrimSpace(getenv("CLAMDSCAN_BINARY"))
	if config.clamAVBinary == "" {
		config.clamAVBinary = "clamdscan"
	}
	config.pollInterval = 2 * time.Second
	config.leaseDuration = 5 * time.Minute
	return config, nil
}

func run(ctx context.Context, config workerConfig) error {
	if !config.enabled {
		slog.Info("Media scanning is disabled")
		<-ctx.Done()
		return nil
	}
	db, err := sql.Open("pgx", config.databaseURL)
	if err != nil {
		return errors.New("media worker database configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		return errors.New("media worker database connection failed")
	}
	store, err := media.NewR2Storage(config.r2, time.Now)
	if err != nil {
		return errors.New("media worker object storage configuration failed")
	}
	processor := media.NewProcessor(media.NewRepository(db), store, media.NewClamAVScanner(config.clamAVBinary, 2*time.Minute), media.ProcessorConfig{
		LeaseDuration: config.leaseDuration,
	})

	for ctx.Err() == nil {
		worked, processErr := processor.ProcessNext(ctx)
		if processErr != nil {
			slog.Warn("Media scan job failed", "code", "processing_error")
		}
		if worked && processErr == nil {
			continue
		}
		timer := time.NewTimer(config.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}

func main() {
	config, err := loadConfig(os.Getenv)
	if err != nil {
		slog.Error("Media worker configuration failed", "reason", err.Error())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config); err != nil {
		slog.Error("Media worker stopped", "reason", err.Error())
		os.Exit(1)
	}
}

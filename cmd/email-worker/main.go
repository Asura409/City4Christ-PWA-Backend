// email-worker dispatches the existing verification outbox. It never runs
// migrations or resets data; schema changes are a separate operational step.
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/mailresend"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type workerConfig struct {
	enabled         bool
	environment     string
	databaseURL     string
	verificationKey []byte
	apiKey          string
	from            string
	recipients      map[string]bool
}

func loadConfig(getenv func(string) string) (workerConfig, error) {
	var config workerConfig
	mode := getenv("EMAIL_DELIVERY_MODE")
	if mode == "" || mode == "disabled" {
		return config, nil
	}
	if mode != "resend" {
		return config, errors.New("EMAIL_DELIVERY_MODE must be disabled or resend")
	}
	config.enabled = true
	config.environment = getenv("APP_ENV")
	if config.environment != "production" && config.environment != "staging" {
		return workerConfig{}, errors.New("APP_ENV must be staging or production for live delivery")
	}
	config.databaseURL = getenv("DATABASE_URL")
	if strings.TrimSpace(config.databaseURL) == "" {
		return workerConfig{}, errors.New("DATABASE_URL is required")
	}
	if _, err := pgx.ParseConfig(config.databaseURL); err != nil {
		return workerConfig{}, errors.New("DATABASE_URL is invalid")
	}
	key, err := base64.StdEncoding.DecodeString(getenv("VERIFICATION_KEY_BASE64"))
	if err != nil || len(key) != 32 {
		return workerConfig{}, errors.New("VERIFICATION_KEY_BASE64 must encode 32 bytes")
	}
	config.verificationKey = key
	config.apiKey = getenv("RESEND_API_KEY")
	config.from = getenv("EMAIL_FROM")
	if _, err := mailresend.New(config.apiKey, config.from); err != nil {
		return workerConfig{}, errors.New("RESEND_API_KEY and a valid EMAIL_FROM are required")
	}
	config.recipients = make(map[string]bool)
	// A standalone wildcard explicitly enables email to all beta recipients.
	if strings.TrimSpace(getenv("EMAIL_RECIPIENT_ALLOWLIST")) == "*" {
		config.recipients["*"] = true
		return config, nil
	}
	for _, entry := range strings.Split(getenv("EMAIL_RECIPIENT_ALLOWLIST"), ",") {
		email := strings.ToLower(strings.TrimSpace(entry))
		if email == "" {
			continue
		}
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			return workerConfig{}, errors.New("EMAIL_RECIPIENT_ALLOWLIST contains an invalid address")
		}
		config.recipients[email] = true
	}
	if config.environment == "staging" && len(config.recipients) == 0 {
		return workerConfig{}, errors.New("staging live delivery requires EMAIL_RECIPIENT_ALLOWLIST")
	}
	return config, nil
}

func (c workerConfig) allowed(email string) bool {
	if c.recipients["*"] {
		return true
	}
	if len(c.recipients) > 0 {
		return c.recipients[strings.ToLower(strings.TrimSpace(email))]
	}
	return c.environment == "production"
}

func blockedRecipientError() *auth.EmailDeliveryError {
	return &auth.EmailDeliveryError{Code: "provider_rejected"}
}

type guardedSender struct {
	config workerConfig
	sender auth.VerificationEmailSender
}

func (s guardedSender) Send(ctx context.Context, id string, email auth.VerificationEmail) error {
	if !s.config.allowed(email.Email) {
		return blockedRecipientError()
	}
	return s.sender.Send(ctx, id, email)
}

func run(ctx context.Context, config workerConfig) error {
	if !config.enabled {
		slog.Info("Email delivery is disabled")
		<-ctx.Done()
		return nil
	}
	db, err := sql.Open("pgx", config.databaseURL)
	if err != nil {
		return errors.New("email worker database configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		return errors.New("email worker database connection failed")
	}
	verification, err := auth.NewVerificationService(db, auth.VerificationConfig{Key: config.verificationKey})
	if err != nil {
		return errors.New("email worker verification configuration failed")
	}
	sender, err := mailresend.New(config.apiKey, config.from)
	if err != nil {
		return errors.New("email worker sender configuration failed")
	}
	guarded := guardedSender{config: config, sender: sender}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		_, err := verification.DeliverNext(ctx, guarded)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			code := "storage_unavailable"
			var failure *auth.EmailDeliveryError
			if errors.As(err, &failure) {
				code = failure.Code
			}
			slog.Warn("Verification email processing failed", "code", code)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func main() {
	config, err := loadConfig(os.Getenv)
	if err != nil {
		slog.Error("Email worker configuration failed", "reason", err.Error())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config); err != nil {
		slog.Error("Email worker stopped", "reason", err.Error())
		os.Exit(1)
	}
}

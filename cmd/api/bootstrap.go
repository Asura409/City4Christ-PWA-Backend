package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/google/uuid"
)

type bootstrapOptions struct{ accountID, reason string }

func parseBootstrapOptions(args []string) (bootstrapOptions, error) {
	var options bootstrapOptions
	var confirm string
	flags := flag.NewFlagSet("bootstrap-super-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.accountID, "account-id", "", "Approved verified account UUID")
	flags.StringVar(&confirm, "confirm-account-id", "", "Repeat the approved account UUID")
	flags.StringVar(&options.reason, "reason", "", "Appointment approval reason")
	invalid := errors.New("usage: api bootstrap-super-admin --account-id UUID --confirm-account-id SAME_UUID --reason TEXT")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return bootstrapOptions{}, invalid
	}
	id, err := uuid.Parse(options.accountID)
	options.reason = strings.TrimSpace(options.reason)
	if err != nil || id == uuid.Nil || id.String() != options.accountID || confirm != options.accountID || !utf8.ValidString(options.reason) || strings.ContainsRune(options.reason, 0) || utf8.RuneCountInString(options.reason) < 1 || utf8.RuneCountInString(options.reason) > 500 {
		return bootstrapOptions{}, invalid
	}
	return options, nil
}

func bootstrapSuperAdmin(ctx context.Context, db *sql.DB, options bootstrapOptions) error {
	err := access.NewService(db).BootstrapChurchAdmin(ctx, options.accountID, options.reason)
	switch {
	case errors.Is(err, access.ErrBootstrapConflict):
		return errors.New("Super Admin bootstrap already completed or an administrator exists; use protected role management")
	case errors.Is(err, access.ErrInvalid):
		return errors.New("bootstrap requires an existing verified account with confirmed MFA and a valid approval reason")
	case err != nil:
		return errors.New("Super Admin bootstrap failed; verify database availability and applied migrations")
	}
	slog.Info("Super Admin bootstrap complete", "account_id", options.accountID)
	return nil
}

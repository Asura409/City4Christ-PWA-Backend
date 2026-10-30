package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
)

func loadPastoralConfig(c *workerConfig, getenv func(string) string) error {
	switch getenv("PASTORAL_EMAIL_ENABLED") {
	case "", "false":
		return nil
	case "true":
		c.pastoralEnabled = true
	default:
		return errors.New("PASTORAL_EMAIL_ENABLED must be true or false")
	}
	c.pastoralAppURL = getenv("PASTORAL_EMAIL_APP_URL")
	if _, err := pastoral.AppointmentAppURL(c.pastoralAppURL); err != nil {
		return errors.New("PASTORAL_EMAIL_APP_URL must be an HTTPS frontend address without credentials, query or fragment")
	}
	return nil
}
func newPastoralWorker(db *sql.DB, c workerConfig) (*pastoral.EmailWorker, error) {
	if !c.pastoralEnabled {
		return nil, nil
	}
	return pastoral.NewEmailWorker(db, c.pastoralAppURL, nil)
}

type guardedAppointmentSender struct {
	config workerConfig
	sender pastoral.AppointmentEmailSender
}

func (s guardedAppointmentSender) SendAppointment(ctx context.Context, id string, m pastoral.AppointmentEmail) error {
	if !s.config.allowed(m.Email) {
		return blockedRecipientError()
	}
	return s.sender.SendAppointment(ctx, id, m)
}
func processPastoralEmail(ctx context.Context, w *pastoral.EmailWorker, sender pastoral.AppointmentEmailSender) {
	if w == nil {
		return
	}
	if _, err := w.DeliverNext(ctx, sender); err != nil && ctx.Err() == nil {
		code := "storage_unavailable"
		var failure *auth.EmailDeliveryError
		if errors.As(err, &failure) {
			code = failure.Code
		}
		slog.Warn("Appointment email processing failed", "code", code)
	}
}

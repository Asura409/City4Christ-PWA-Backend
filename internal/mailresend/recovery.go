package mailresend

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/google/uuid"
	"github.com/resend/resend-go/v4"
)

//go:embed recovery.html
var recoveryHTML string
var recoveryTemplate = template.Must(template.New("recovery").Parse(recoveryHTML))

func (s *Sender) SendRecovery(ctx context.Context, id string, m auth.RecoveryEmail) error {
	invalid := func() error { return &auth.EmailDeliveryError{Code: "invalid_email_payload"} }
	a, err := mail.ParseAddress(m.Email)
	if err != nil || a.Address != m.Email || m.ExpiresAt.IsZero() {
		return invalid()
	}
	if _, err := uuid.Parse(id); err != nil {
		return invalid()
	}
	title := "Reset your City4Christ password"
	description := "Use the button below to choose a new password. This link expires in fifteen minutes and can be used once."
	text := ""
	if m.PasswordChanged {
		if m.ResetURL != "" {
			return invalid()
		}
		title = "Your City4Christ password changed"
		description = "Your password was changed and all previous sign-in sessions were ended. If you did not make this change, contact your church administrator immediately."
		text = description
	} else {
		u, err := url.Parse(m.ResetURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || !strings.HasSuffix(u.Path, "/auth.html") || !strings.HasPrefix(u.Fragment, "reset_token=") || len(strings.TrimPrefix(u.Fragment, "reset_token=")) != 43 {
			return invalid()
		}
		text = fmt.Sprintf("Reset your City4Christ password:\n%s\n\nExpires at %s. If you did not request this, ignore this email. Your password has not changed.\n", m.ResetURL, m.ExpiresAt.UTC().Format(time.RFC3339))
	}
	var html bytes.Buffer
	if err := recoveryTemplate.Execute(&html, struct{ Title, Description, ResetURL, Expiry string }{title, description, m.ResetURL, m.ExpiresAt.UTC().Format("2 Jan 2006 at 15:04 UTC")}); err != nil {
		return invalid()
	}
	sent, err := s.client.Emails.SendWithOptions(ctx, &resend.SendEmailRequest{From: s.from, To: []string{m.Email}, Subject: title, Text: text, Html: html.String()}, &resend.SendEmailOptions{IdempotencyKey: "password-recovery/" + id})
	if err != nil {
		var failure *auth.EmailDeliveryError
		if errors.As(err, &failure) {
			return failure
		}
		return &auth.EmailDeliveryError{Code: "provider_unavailable", Retryable: true}
	}
	if sent == nil || sent.Id == "" {
		return &auth.EmailDeliveryError{Code: "provider_unavailable", Retryable: true}
	}
	return nil
}

package mailresend

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"html/template"
	"net/mail"
	"net/url"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"github.com/google/uuid"
	"github.com/resend/resend-go/v4"
)

//go:embed appointment.html
var appointmentHTML string
var appointmentTemplate = template.Must(template.New("appointment").Parse(appointmentHTML))

func (s *Sender) SendAppointment(ctx context.Context, id string, m pastoral.AppointmentEmail) error {
	invalid := func() error { return &auth.EmailDeliveryError{Code: "invalid_email_payload"} }
	address, err := mail.ParseAddress(m.Email)
	if err != nil || address.Address != m.Email {
		return invalid()
	}
	if parsed, err := uuid.Parse(id); err != nil || parsed == uuid.Nil {
		return invalid()
	}
	u, err := url.Parse(m.AppURL)
	if err != nil || u.Fragment != "/pastors" {
		return invalid()
	}
	u.Fragment = ""
	if _, err = pastoral.AppointmentAppURL(u.String()); err != nil {
		return invalid()
	}
	description := "An update is ready in your City4Christ account. Sign in to view the details."
	if m.Reminder {
		description = "You have an upcoming appointment. Sign in to review the details."
	}
	var html bytes.Buffer
	if err = appointmentTemplate.Execute(&html, struct{ Description, AppURL string }{description, m.AppURL}); err != nil {
		return invalid()
	}
	sent, err := s.client.Emails.SendWithOptions(ctx, &resend.SendEmailRequest{From: s.from, To: []string{m.Email}, Subject: "Your City4Christ account update", Text: description + "\n\nOpen your account: " + m.AppURL + "\n", Html: html.String()}, &resend.SendEmailOptions{IdempotencyKey: "pastoral-email/" + id})
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

package main

import (
	"context"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
)

func TestPastoralEmailConfigurationIsExplicitAndValidatesFrontendURL(t *testing.T) {
	// Disabled main delivery must remain inert, even with irrelevant malformed settings.
	c, err := loadConfig(func(k string) string {
		if k == "PASTORAL_EMAIL_ENABLED" {
			return "true"
		}
		return ""
	})
	if err != nil || c.enabled {
		t.Fatal(c, err)
	}
	values := validWorkerEnvironment()
	for _, u := range []string{"", "http://frontend.example", "https://user:pass@frontend.example", "https://frontend.example?token=x"} {
		values["PASTORAL_EMAIL_APP_URL"] = u
		values["PASTORAL_EMAIL_ENABLED"] = "true"
		if _, err := loadConfig(func(k string) string { return values[k] }); err == nil {
			t.Fatal("invalid enabled pastoral URL", u)
		}
	}
	values["PASTORAL_EMAIL_APP_URL"] = "https://frontend.example/City4Christ-PWA/"
	c, err = loadConfig(func(k string) string { return values[k] })
	if err != nil || !c.pastoralEnabled {
		t.Fatal("pastoral config", err)
	}
	values["PASTORAL_EMAIL_ENABLED"] = "typo"
	if _, err := loadConfig(func(k string) string { return values[k] }); err == nil {
		t.Fatal("invalid boolean")
	}
}
func validWorkerEnvironment() map[string]string {
	return map[string]string{"EMAIL_DELIVERY_MODE": "resend", "APP_ENV": "production", "DATABASE_URL": "postgres://localhost/test", "VERIFICATION_KEY_BASE64": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "RESEND_API_KEY": "test-key", "EMAIL_FROM": "info@example.invalid"}
}

type appointmentSenderStub struct{ calls int }

func (s *appointmentSenderStub) SendAppointment(context.Context, string, pastoral.AppointmentEmail) error {
	s.calls++
	return nil
}
func TestPastoralEmailUsesExistingRecipientPolicy(t *testing.T) {
	provider := &appointmentSenderStub{}
	s := guardedAppointmentSender{config: workerConfig{environment: "staging", recipients: map[string]bool{"tester@example.invalid": true}}, sender: provider}
	if err := s.SendAppointment(context.Background(), "id", pastoral.AppointmentEmail{Email: "blocked@example.invalid"}); err == nil || provider.calls != 0 {
		t.Fatal("recipient guard bypassed")
	}
	if err := s.SendAppointment(context.Background(), "id", pastoral.AppointmentEmail{Email: "tester@example.invalid"}); err != nil || provider.calls != 1 {
		t.Fatal("allowlisted recipient blocked", err)
	}
}

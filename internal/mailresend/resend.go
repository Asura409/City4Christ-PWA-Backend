// Package mailresend sends verification emails through the official Resend SDK.
package mailresend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/google/uuid"
	"github.com/resend/resend-go/v4"
)

type Sender struct {
	client *resend.Client
	from   string
}

func New(apiKey, from string) (*Sender, error) {
	return newSender(apiKey, from, &http.Client{Timeout: 10 * time.Second})
}

func newSender(apiKey, from string, httpClient *http.Client) (*Sender, error) {
	address, err := mail.ParseAddress(from)
	if strings.TrimSpace(apiKey) == "" || strings.ContainsAny(apiKey, "\r\n") || err != nil || !strings.Contains(address.Address, "@") || strings.ContainsAny(from, "\r\n") {
		return nil, errors.New("Resend API key and valid sender are required")
	}
	clientCopy := *httpClient
	if clientCopy.Timeout <= 0 || clientCopy.Timeout > 10*time.Second {
		clientCopy.Timeout = 10 * time.Second
	}
	transport := clientCopy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clientCopy.Transport = safeTransport{inner: transport}
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	sdk := resend.NewCustomClient(&clientCopy, apiKey)
	// Ignore SDK endpoint environment overrides: credentials only go to Resend.
	sdk.BaseURL = &url.URL{Scheme: "https", Host: "api.resend.com", Path: "/"}
	return &Sender{client: sdk, from: from}, nil
}

func (s *Sender) Send(ctx context.Context, challengeID string, message auth.VerificationEmail) error {
	address, err := mail.ParseAddress(message.Email)
	validCode := len(message.Code) == 8
	for _, digit := range message.Code {
		if digit < '0' || digit > '9' {
			validCode = false
		}
	}
	if _, errID := uuid.Parse(challengeID); errID != nil || err != nil || address.Address != message.Email || !validCode || message.ExpiresAt.IsZero() {
		return &auth.EmailDeliveryError{Code: "invalid_email_payload"}
	}
	htmlBody, err := renderVerificationHTML(message)
	if err != nil {
		return &auth.EmailDeliveryError{Code: "invalid_email_payload"}
	}
	params := &resend.SendEmailRequest{
		Html: htmlBody,
		From: s.from, To: []string{message.Email}, Subject: "Verify your City4Christ email",
		Text: fmt.Sprintf("Your City4Christ verification code is: %s\n\nThis code expires at %s.\nIf you did not request this, you can ignore this email.\n", message.Code, message.ExpiresAt.UTC().Format(time.RFC3339)),
	}
	sent, err := s.client.Emails.SendWithOptions(ctx, params, &resend.SendEmailOptions{IdempotencyKey: "email-verification/" + challengeID})
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

// The SDK currently discards most HTTP status codes in its errors. Classify at
// the transport boundary and discard response bodies before they can leak.
type safeTransport struct{ inner http.RoundTripper }

func (t safeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.inner.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	response.Body.Close()
	failure := &auth.EmailDeliveryError{Code: "provider_rejected"}
	switch {
	case response.StatusCode == 429:
		failure.Code = "provider_rate_limited"
		failure.Retryable = true
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 86400 {
			failure.RetryAfter = time.Duration(seconds) * time.Second
		}
	case response.StatusCode >= 500 || response.StatusCode == 409 || response.StatusCode == 408:
		failure.Code = "provider_unavailable"
		failure.Retryable = true
	}
	return nil, failure
}

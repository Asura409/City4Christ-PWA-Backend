package auth

import (
	"context"
	"time"
)

// VerificationEmailSender accepts one immutable email per challenge identifier.
// Implementations must use that identifier as the provider idempotency key.
type VerificationEmailSender interface {
	Send(context.Context, string, VerificationEmail) error
}

// EmailDeliveryError carries only safe classifications, never provider payloads.
type EmailDeliveryError struct {
	Code       string
	Retryable  bool
	RetryAfter time.Duration
}

func (e *EmailDeliveryError) Error() string { return "email delivery: " + e.Code }

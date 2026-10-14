package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func verificationHandler(service *auth.VerificationService, limiter *auth.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
			return
		}
		input, ok := decodeJSON[struct {
			Email string `json:"email"`
			Code  string `json:"code"`
		}](w, r)
		if !ok {
			return
		}
		fields := make(map[string]string)
		if strings.TrimSpace(input.Email) == "" {
			fields["email"] = "Enter your email address."
		}
		if input.Code == "" {
			fields["code"] = "Enter the verification code."
		}
		if len(fields) > 0 {
			writeError(w, 422, "validation_failed", "Check the supplied fields.", fields)
			return
		}
		if !accountLimit(w, r, limiter, "verify", input.Email, 20, 15*time.Minute) {
			return
		}
		err := service.Verify(r.Context(), input.Email, input.Code)
		if errors.Is(err, auth.ErrInvalidOTP) {
			writeError(w, 400, "invalid_verification_code", "Code is invalid or expired.", nil)
			return
		}
		if err != nil {
			writeError(w, 500, "internal_error", "Request could not be completed.", nil)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "email_verified"}})
	}
}

func resendHandler(service *auth.VerificationService, limiter *auth.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
			return
		}
		input, ok := decodeJSON[struct {
			Email string `json:"email"`
		}](w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(input.Email) == "" {
			writeError(w, 422, "validation_failed", "Check the supplied fields.", map[string]string{"email": "Enter your email address."})
			return
		}
		if !accountLimit(w, r, limiter, "resend", input.Email, 5, 15*time.Minute) {
			return
		}
		if err := service.Resend(r.Context(), input.Email); err != nil && !errors.Is(err, auth.ErrResendCooldown) {
			writeError(w, 500, "internal_error", "Request could not be completed.", nil)
			return
		}
		writeJSON(w, 202, map[string]any{"data": map[string]string{"message": "If verification is needed, an email will be sent when eligible."}})
	}
}

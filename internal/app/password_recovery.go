package app

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func passwordRecoveryRoutes(mux *http.ServeMux, service *auth.PasswordRecoveryService, limiter *auth.RateLimiter, c Config) {
	available := func(w http.ResponseWriter) bool {
		if service == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/v1/auth/password/forgot", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		input, ok := decodeJSON[struct {
			Email    string `json:"email"`
			ReturnTo string `json:"returnTo"`
		}](w, r)
		if !ok {
			return
		}
		email := strings.ToLower(strings.TrimSpace(input.Email))
		address, err := mail.ParseAddress(email)
		fields := map[string]string{}
		if err != nil || address.Address != email || !strings.Contains(email, "@") || len(email) > 254 {
			fields["email"] = "Enter a valid email address."
		}
		if !googleReturnAllowed(c, input.ReturnTo) {
			fields["returnTo"] = "Use an allowed HTTPS frontend auth.html URL without query or fragment."
		}
		if len(fields) > 0 {
			writeError(w, 422, "validation_failed", "Check the supplied fields.", fields)
			return
		}
		if !accountLimit(w, r, limiter, "password-forgot", email, 5, 15*time.Minute) {
			return
		}
		if err := service.Request(r.Context(), email, input.ReturnTo); err != nil {
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
			return
		}
		writeJSON(w, 202, map[string]any{"data": map[string]string{"message": "If this email has an eligible password account, a reset link will be sent when eligible."}})
	})
	mux.HandleFunc("POST /api/v1/auth/password/reset", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		input, ok := decodeJSON[struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}](w, r)
		if !ok {
			return
		}
		if len(input.Token) != 43 {
			writeError(w, 400, "invalid_reset_token", "Reset link is invalid or expired. Request a new one.", nil)
			return
		}
		if !accountLimit(w, r, limiter, "password-reset", input.Token, 10, 15*time.Minute) {
			return
		}
		err := service.Reset(r.Context(), input.Token, input.Password)
		var validation *auth.ValidationError
		switch {
		case errors.As(err, &validation):
			writeError(w, 422, "validation_failed", "Check the supplied fields.", validation.Fields)
		case errors.Is(err, auth.ErrInvalidResetToken):
			writeError(w, 400, "invalid_reset_token", "Reset link is invalid or expired. Request a new one.", nil)
		case err != nil:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		default:
			clearRefresh(w, c)
			writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "password_reset"}})
		}
	})
}

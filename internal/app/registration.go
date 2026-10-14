package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func registrationHandler(service *auth.RegistrationService, enabled bool, limiter *auth.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			writeError(w, 403, "registration_closed", "Registration is currently closed.", nil)
			return
		}
		if service == nil {
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
			return
		}
		input, ok := decodeJSON[auth.Registration](w, r)
		if !ok {
			return
		}
		if !accountLimit(w, r, limiter, "register", input.Email, 5, 15*time.Minute) {
			return
		}
		account, err := service.Register(r.Context(), input)
		if err != nil {
			var validation *auth.ValidationError
			switch {
			case errors.As(err, &validation):
				writeError(w, 422, "validation_failed", "Check the supplied fields.", validation.Fields)
			case errors.Is(err, auth.ErrEmailTaken):
				writeError(w, 409, "email_unavailable", "This email cannot be registered.", nil)
			default:
				writeError(w, 500, "internal_error", "Request could not be completed.", nil)
			}
			return
		}
		writeJSON(w, 201, map[string]any{"data": map[string]any{"id": account.ID, "email": account.Email, "firstName": account.FirstName, "lastName": account.LastName, "status": "email_unverified"}})
	}
}

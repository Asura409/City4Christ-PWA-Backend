// Package app owns the global HTTP router and dependency wiring.
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/google/uuid"
)

type Config struct {
	RegistrationEnabled bool
	VerificationKey     []byte
	Now                 func() time.Time
}

func New(db *sql.DB, config Config) http.Handler {
	mux := http.NewServeMux()
	verification, verificationErr := auth.NewVerificationService(db, auth.VerificationConfig{Key: config.VerificationKey, Now: config.Now})
	var service *auth.RegistrationService
	// Invalid/missing auth secrets leave health routes usable and auth unavailable.
	if verificationErr == nil {
		service = auth.NewRegistrationService(auth.NewPasswordHasher(), verification)
	}
	mux.HandleFunc("POST /api/v1/auth/register", registrationHandler(service, config.RegistrationEnabled))
	mux.HandleFunc("POST /api/v1/auth/verification/verify", verificationHandler(verification))
	mux.HandleFunc("POST /api/v1/auth/verification/resend", resendHandler(verification))
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate server-controlled correlation IDs; never reflect arbitrary input.
		w.Header().Set("X-Request-Id", uuid.NewString())
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

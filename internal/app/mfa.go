package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func mfaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrMFACode):
		writeError(w, 400, "invalid_mfa_code", "The authenticator or recovery code is invalid or expired.", nil)
	case errors.Is(err, auth.ErrMFAConflict):
		writeError(w, 409, "mfa_already_enabled", "Confirm your existing authenticator before changing it.", nil)
	default:
		sessionError(w, err)
	}
}
func mfaRoutes(mux *http.ServeMux, mfa *auth.MFAService, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	authorize := func(w http.ResponseWriter, r *http.Request, scope string, limit int, window time.Duration) bool {
		if mfa == nil || sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Authenticator confirmation is not configured.", nil)
			return false
		}
		a, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return false
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 422, "validation_failed", "This endpoint accepts no query parameters.", nil)
			return false
		}
		if !accountLimit(w, r, limiter, scope, a.ID, limit, window) {
			return false
		}
		if r.Method == "GET" && limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/mfa-status", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, err) {
				return false
			}
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/auth/mfa", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, "mfa-status", 60, time.Minute) {
			return
		}
		status, err := mfa.Status(r.Context(), requestAccess(r))
		if err != nil {
			mfaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": status})
	})
	mux.HandleFunc("POST /api/v1/auth/mfa/enroll", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, "mfa-management", 5, 15*time.Minute) {
			return
		}
		if _, ok := decodeJSON[struct{}](w, r); !ok {
			return
		}
		setup, err := mfa.Begin(r.Context(), requestAccess(r))
		if err != nil {
			mfaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": setup})
	})
	mux.HandleFunc("POST /api/v1/auth/mfa/confirm", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, "mfa-attempt", 5, 15*time.Minute) {
			return
		}
		in, ok := decodeJSON[struct {
			EnrollmentID string `json:"enrollmentId"`
			Code         string `json:"code"`
		}](w, r)
		if !ok {
			return
		}
		codes, err := mfa.Confirm(r.Context(), requestAccess(r), in.EnrollmentID, in.Code)
		if err != nil {
			mfaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"recoveryCodes": codes}})
	})
	for _, recovery := range []bool{false, true} {
		path := "POST /api/v1/auth/mfa/verify"
		if recovery {
			path = "POST /api/v1/auth/mfa/recovery"
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !authorize(w, r, "mfa-attempt", 5, 15*time.Minute) {
				return
			}
			var code string
			if recovery {
				in, ok := decodeJSON[struct {
					RecoveryCode string `json:"recoveryCode"`
				}](w, r)
				if !ok {
					return
				}
				code = in.RecoveryCode
			} else {
				in, ok := decodeJSON[struct {
					Code string `json:"code"`
				}](w, r)
				if !ok {
					return
				}
				code = in.Code
			}
			if err := mfa.Verify(r.Context(), requestAccess(r), code, recovery); err != nil {
				mfaError(w, err)
				return
			}
			status, err := mfa.Status(r.Context(), requestAccess(r))
			if err != nil {
				mfaError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"data": status})
		})
	}
	mux.HandleFunc("POST /api/v1/auth/mfa/recovery/regenerate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, "mfa-management", 5, 15*time.Minute) {
			return
		}
		if _, ok := decodeJSON[struct{}](w, r); !ok {
			return
		}
		codes, err := mfa.RegenerateRecovery(r.Context(), requestAccess(r))
		if err != nil {
			mfaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"recoveryCodes": codes}})
	})
	mux.HandleFunc("POST /api/v1/auth/mfa/disable", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, "mfa-management", 5, 15*time.Minute) {
			return
		}
		if _, ok := decodeJSON[struct{}](w, r); !ok {
			return
		}
		if err := mfa.Disable(r.Context(), requestAccess(r)); err != nil {
			mfaError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "disabled"}})
	})
}

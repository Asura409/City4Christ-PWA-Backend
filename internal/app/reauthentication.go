package app

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func reauthenticationRoutes(mux *http.ServeMux, sessions *auth.SessionService, google *auth.GoogleService, limiter *auth.RateLimiter, c Config) {
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) bool {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
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
		scope, limit, window := "reauthentication-status", 60, time.Minute
		if write {
			scope, limit, window = "reauthentication", 5, 15*time.Minute
		}
		if !accountLimit(w, r, limiter, scope, a.ID, limit, window) {
			return false
		}
		if !write && limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/reauthentication-status", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, err) {
				return false
			}
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/auth/reauthentication", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, false) {
			return
		}
		status, err := sessions.ReauthenticationStatus(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return
		}
		if google == nil {
			status.GoogleAvailable = false
		}
		writeJSON(w, 200, map[string]any{"data": status})
	})
	mux.HandleFunc("POST /api/v1/auth/reauthentication/password", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, true) {
			return
		}
		in, ok := decodeJSON[struct {
			Password string `json:"password"`
		}](w, r)
		if !ok {
			return
		}
		status, err := sessions.ReauthenticatePassword(r.Context(), requestAccess(r), in.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, 401, "invalid_credentials", "Password is incorrect.", nil)
			return
		}
		if err != nil {
			sessionError(w, err)
			return
		}
		if google == nil {
			status.GoogleAvailable = false
		}
		writeJSON(w, 200, map[string]any{"data": status})
	})
	mux.HandleFunc("POST /api/v1/auth/reauthentication/google", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, true) {
			return
		}
		if google == nil {
			writeError(w, 503, "unavailable", "Google authentication is not configured.", nil)
			return
		}
		in, ok := decodeJSON[struct {
			ReturnTo string `json:"returnTo"`
		}](w, r)
		if !ok {
			return
		}
		if in.ReturnTo != "" && !googleReturnAllowed(c, in.ReturnTo) {
			writeError(w, 422, "validation_failed", "Choose an allowed frontend sign-in page.", nil)
			return
		}
		binding := make([]byte, 32)
		if _, err := rand.Read(binding); err != nil {
			sessionError(w, err)
			return
		}
		raw := base64.RawURLEncoding.EncodeToString(binding)
		flow, err := google.BeginReauthentication(r.Context(), raw, requestAccess(r), in.ReturnTo)
		if err != nil {
			if err == auth.ErrInvalidOIDC {
				writeError(w, 400, "invalid_oidc", "Use a Google account already linked to this account.", nil)
			} else {
				sessionError(w, err)
			}
			return
		}
		sameSite := http.SameSiteLaxMode
		if c.CrossSiteCookies {
			sameSite = http.SameSiteNoneMode
		}
		http.SetCookie(w, &http.Cookie{Name: oidcCookieName, Value: raw, Secure: true, HttpOnly: true, SameSite: sameSite, Path: "/", MaxAge: 300, Expires: flow.ExpiresAt})
		writeJSON(w, 200, map[string]any{"data": map[string]string{"url": flow.URL}})
	})
}

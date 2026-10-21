package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

const refreshCookieName = "__Host-c4c_refresh"

func requestAccess(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}
func requestRefresh(r *http.Request) string {
	cookies := r.CookiesNamed(refreshCookieName)
	if len(cookies) != 1 {
		return ""
	}
	return cookies[0].Value
}
func sessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrMFARequired):
		writeError(w, 403, "mfa_required", "Confirm your authenticator or use a recovery code before continuing.", nil)
	case errors.Is(err, auth.ErrReauthenticationRequired):
		writeError(w, 403, "reauthentication_required", "Confirm your identity before continuing.", nil)
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, 401, "invalid_credentials", "Email or password is incorrect.", nil)
	case errors.Is(err, auth.ErrEmailUnverified):
		writeError(w, 403, "email_unverified", "Verify your email before signing in.", nil)
	case errors.Is(err, auth.ErrInvalidSession), errors.Is(err, auth.ErrRefreshReplay):
		writeError(w, 401, "invalid_session", "Sign in again.", nil)
	default:
		writeError(w, 503, "unavailable", "Service unavailable.", nil)
	}
}
func setRefresh(w http.ResponseWriter, c Config, raw string, expires time.Time) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	maxAge := int(expires.Sub(now).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	sameSite := http.SameSiteLaxMode
	if c.CrossSiteCookies {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{Name: refreshCookieName, Value: raw, Path: "/", Secure: true, HttpOnly: true, SameSite: sameSite, Expires: expires, MaxAge: maxAge})
}
func clearRefresh(w http.ResponseWriter, c Config) {
	sameSite := http.SameSiteLaxMode
	if c.CrossSiteCookies {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{Name: refreshCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: sameSite, Expires: time.Unix(1, 0), MaxAge: -1})
}
func tokenResponse(w http.ResponseWriter, c Config, tokens auth.SessionTokens) {
	setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"data": map[string]any{"accessToken": tokens.AccessToken, "tokenType": "Bearer", "expiresIn": int(tokens.AccessExpiresAt.Sub(now).Seconds())}})
}
func sessionRoutes(mux *http.ServeMux, service *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	available := func(w http.ResponseWriter) bool {
		if service == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		input, ok := decodeJSON[struct{ Email, Password string }](w, r)
		if !ok {
			return
		}
		if !accountLimit(w, r, limiter, "login", input.Email, 10, 15*time.Minute) {
			return
		}
		tokens, err := service.Login(r.Context(), input.Email, input.Password)
		if err != nil {
			sessionError(w, err)
			return
		}
		tokenResponse(w, c, tokens)
	})
	mux.HandleFunc("POST /api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		tokens, err := service.Refresh(r.Context(), requestRefresh(r))
		if err != nil {
			clearRefresh(w, c)
			sessionError(w, err)
			return
		}
		tokenResponse(w, c, tokens)
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		if err := service.Logout(r.Context(), requestRefresh(r)); err != nil {
			sessionError(w, err)
			return
		}
		clearRefresh(w, c)
		writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "logged_out"}})
	})
}

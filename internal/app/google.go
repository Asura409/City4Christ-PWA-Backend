package app

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

const oidcCookieName = "__Host-c4c_oidc"

func googleRoutes(mux *http.ServeMux, google *auth.GoogleService, c Config) {
	available := func(w http.ResponseWriter) bool {
		if google == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Google authentication is not configured.", nil)
			return false
		}
		return true
	}
	begin := func(link bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !available(w) {
				return
			}
			target := r.URL.Query().Get("returnTo")
			if len(r.URL.Query()["returnTo"]) > 1 || (target != "" && !googleReturnAllowed(c, target)) || (r.URL.Query().Has("redirect") && (link || target == "" || r.URL.Query().Get("redirect") != "1" || len(r.URL.Query()["redirect"]) != 1)) {
				writeError(w, 422, "validation_failed", "Choose an allowed frontend sign-in page.", map[string]string{"returnTo": "Use an allowed HTTPS origin and its auth.html page, without query or fragment."})
				return
			}
			access := ""
			if link {
				access = requestAccess(r)
				if access == "" {
					sessionError(w, auth.ErrInvalidSession)
					return
				}
			}
			random := make([]byte, 32)
			if _, err := rand.Read(random); err != nil {
				sessionError(w, err)
				return
			}
			binding := base64.RawURLEncoding.EncodeToString(random)
			flow, err := google.BeginWithReturn(r.Context(), binding, access, target)
			if err != nil {
				sessionError(w, err)
				return
			}
			sameSite := http.SameSiteLaxMode
			if c.CrossSiteCookies {
				sameSite = http.SameSiteNoneMode
			}
			http.SetCookie(w, &http.Cookie{Name: oidcCookieName, Value: binding, Secure: true, HttpOnly: true, SameSite: sameSite, Path: "/", MaxAge: 600, Expires: flow.ExpiresAt})
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Query().Get("redirect") == "1" {
				http.Redirect(w, r, flow.URL, http.StatusFound)
				return
			}
			writeJSON(w, 200, map[string]any{"data": map[string]string{"url": flow.URL}})
		}
	}
	mux.HandleFunc("GET /api/v1/auth/google/start", begin(false))
	mux.HandleFunc("POST /api/v1/auth/google/link", begin(true))
	mux.HandleFunc("GET /api/v1/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.SetCookie(w, &http.Cookie{Name: oidcCookieName, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0)})
		cookies := r.CookiesNamed(oidcCookieName)
		if len(cookies) != 1 || r.URL.Query().Get("error") != "" || len(r.URL.Query()["state"]) != 1 || len(r.URL.Query()["code"]) != 1 {
			writeError(w, 400, "invalid_oidc", "Google sign-in could not be completed.", nil)
			return
		}
		if handleGoogleReauthentication(w, r, google, c, cookies[0].Value) {
			return
		}
		tokens, target, err := google.CompleteWithReturn(r.Context(), r.URL.Query().Get("state"), cookies[0].Value, r.URL.Query().Get("code"))
		if target != "" {
			if !googleReturnAllowed(c, target) {
				writeError(w, 400, "invalid_oidc", "The frontend return URL is no longer allowed.", nil)
				return
			}
			if err == nil {
				setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
			code := "google_sign_in_failed"
			switch {
			case errors.Is(err, auth.ErrLinkRequired):
				code = "account_link_required"
			case errors.Is(err, auth.ErrIdentityConflict):
				code = "identity_conflict"
			case errors.Is(err, auth.ErrRegistrationClosed):
				code = "registration_closed"
			case errors.Is(err, auth.ErrInvalidSession):
				code = "invalid_session"
			case errors.Is(err, auth.ErrMFARequired):
				code = "mfa_required"
			}
			destination, _ := url.Parse(target)
			q := destination.Query()
			q.Set("auth_error", code)
			destination.RawQuery = q.Encode()
			http.Redirect(w, r, destination.String(), http.StatusFound)
			return
		}
		switch {
		case errors.Is(err, auth.ErrLinkRequired):
			writeError(w, 409, "account_link_required", "Sign in to the existing account, then explicitly link Google.", nil)
		case errors.Is(err, auth.ErrIdentityConflict):
			writeError(w, 409, "identity_conflict", "Google identity is already linked.", nil)
		case errors.Is(err, auth.ErrRegistrationClosed):
			writeError(w, 403, "registration_closed", "Registration is currently closed.", nil)
		case errors.Is(err, auth.ErrInvalidOIDC):
			writeError(w, 400, "invalid_oidc", "Google sign-in could not be completed.", nil)
		case err != nil:
			sessionError(w, err)
		default:
			tokenResponse(w, c, tokens)
		}
	})
}

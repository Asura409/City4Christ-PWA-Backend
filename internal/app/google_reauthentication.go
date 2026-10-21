package app

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func handleGoogleReauthentication(w http.ResponseWriter, r *http.Request, google *auth.GoogleService, c Config, browser string) bool {
	handled, target, err := google.CompleteReauthentication(r.Context(), r.URL.Query().Get("state"), browser, r.URL.Query().Get("code"))
	if !handled {
		return false
	}
	if target != "" && googleReturnAllowed(c, target) {
		destination, _ := url.Parse(target)
		q := destination.Query()
		if err == nil {
			q.Set("reauthentication", "complete")
		} else {
			q.Set("auth_error", "reauthentication_failed")
		}
		destination.RawQuery = q.Encode()
		http.Redirect(w, r, destination.String(), http.StatusFound)
		return true
	}
	if err != nil {
		if errors.Is(err, auth.ErrInvalidOIDC) {
			writeError(w, 400, "invalid_oidc", "Google identity confirmation could not be completed.", nil)
		} else {
			sessionError(w, err)
		}
		return true
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "reauthenticated"}})
	return true
}

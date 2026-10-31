package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"net/http"
)

func notificationPushRoutes(mux *http.ServeMux, s *notifications.Service, sessions *auth.SessionService, c Config, authorize notificationAuthorize, fail notificationFailure) {
	enabled := c.PushEnabled && notifications.ValidPushPublicKey(c.PushPublicKey)
	mux.HandleFunc("GET /api/v1/notifications/push/config", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authorize(w, r, false); !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		key := ""
		if enabled {
			key = c.PushPublicKey
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"enabled": enabled, "publicKey": key}})
	})
	mux.HandleFunc("POST /api/v1/notifications/push/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		if !enabled {
			writeError(w, 503, "push_disabled", "Browser push is not configured.", nil)
			return
		}
		in, ok := decodeJSON[notifications.PushSubscription](w, r)
		if !ok {
			return
		}
		_, sid, _, err := sessions.AuthenticateSession(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return
		}
		id, err := s.Subscribe(r.Context(), actor, sid, in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": map[string]string{"id": id}})
	})
	mux.HandleFunc("DELETE /api/v1/notifications/push/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		in, ok := decodeJSON[struct {
			Endpoint string `json:"endpoint"`
		}](w, r)
		if !ok {
			return
		}
		if err := s.Unsubscribe(r.Context(), actor, in.Endpoint); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]bool{"removed": true}})
	})
}

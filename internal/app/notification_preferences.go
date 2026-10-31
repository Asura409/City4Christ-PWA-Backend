package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"net/http"
)

type notificationAuthorize func(http.ResponseWriter, *http.Request, bool) (string, bool)
type notificationFailure func(http.ResponseWriter, error)

func notificationPreferenceRoutes(mux *http.ServeMux, s *notifications.Service, auth notificationAuthorize, fail notificationFailure) {
	mux.HandleFunc("GET /api/v1/notifications/preferences", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		p, err := s.Preferences(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p})
	})
	mux.HandleFunc("PUT /api/v1/notifications/preferences", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		in, ok := decodeJSON[struct {
			PushEnabled   *bool `json:"pushEnabled"`
			Groups        *bool `json:"groups"`
			Events        *bool `json:"events"`
			Pastoral      *bool `json:"pastoral"`
			Messages      *bool `json:"messages"`
			Announcements *bool `json:"announcements"`
		}](w, r)
		if !ok {
			return
		}
		if in.PushEnabled == nil || in.Groups == nil || in.Events == nil || in.Pastoral == nil || in.Messages == nil || in.Announcements == nil {
			fail(w, notifications.ErrInvalid)
			return
		}
		p, err := s.SetPreferences(r.Context(), actor, notifications.Preferences{PushEnabled: *in.PushEnabled, Groups: *in.Groups, Events: *in.Events, Pastoral: *in.Pastoral, Messages: *in.Messages, Announcements: *in.Announcements})
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p})
	})
	mux.HandleFunc("PUT /api/v1/notifications/read-all", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		if _, ok = decodeJSON[struct{}](w, r); !ok {
			return
		}
		n, err := s.MarkAllRead(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]int64{"changedCount": n}})
	})
}

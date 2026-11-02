package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"net/http"
	"net/url"
)

func notificationBroadcastRoutes(mux *http.ServeMux, s *notifications.Service, auth notificationAuthorize, fail notificationFailure) {
	mux.HandleFunc("POST /api/v1/admin/broadcasts", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		in, ok := decodeJSON[notifications.BroadcastInput](w, r)
		if !ok {
			return
		}
		b, err := s.CreateBroadcast(r.Context(), actor, in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": b})
	})
	mux.HandleFunc("GET /api/v1/admin/broadcasts", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, notifications.ErrInvalid)
			return
		}
		for key, values := range q {
			if (key != "groupId" && key != "audience") || len(values) != 1 {
				fail(w, notifications.ErrInvalid)
				return
			}
		}
		audience, groupID := q.Get("audience"), q.Get("groupId")
		if audience == "" {
			audience = "church"
			if groupID != "" {
				audience = "group"
			}
		}
		if (audience == "group") != (groupID != "") {
			fail(w, notifications.ErrInvalid)
			return
		}
		items, err := s.Broadcasts(r.Context(), actor, audience, groupID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": items})
	})
	mux.HandleFunc("GET /api/v1/admin/broadcasts/{broadcastId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		b, err := s.Broadcast(r.Context(), actor, r.PathValue("broadcastId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": b})
	})
	mux.HandleFunc("POST /api/v1/admin/broadcasts/{broadcastId}/send", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		in, ok := decodeJSON[struct {
			Version int  `json:"version"`
			Confirm bool `json:"confirm"`
		}](w, r)
		if !ok {
			return
		}
		if !in.Confirm {
			fail(w, notifications.ErrInvalid)
			return
		}
		b, err := s.SendBroadcast(r.Context(), actor, r.PathValue("broadcastId"), in.Version)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": b})
	})
}

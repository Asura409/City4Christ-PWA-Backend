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
		if err != nil || len(q) > 1 || (len(q) > 0 && (len(q["groupId"]) != 1 || q.Get("groupId") == "")) {
			fail(w, notifications.ErrInvalid)
			return
		}
		items, err := s.Broadcasts(r.Context(), actor, q.Get("groupId"))
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

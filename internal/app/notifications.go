package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
)

func notificationRoutes(mux *http.ServeMux, service *notifications.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, notifications.ErrForbidden):
			writeError(w, 403, "forbidden", "You do not have permission for this notification action.", nil)
		case errors.Is(err, notifications.ErrConflict):
			writeError(w, 409, "state_conflict", "Reload this notification operation before retrying.", nil)
		case errors.Is(err, notifications.ErrInvalid):
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Check the notification pagination or identifier.", nil)
		case errors.Is(err, notifications.ErrNotFound):
			writeError(w, http.StatusNotFound, "notification_not_found", "Notification not found.", nil)
		default:
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Notifications are not configured.", nil)
			return "", false
		}
		if write && !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, http.StatusForbidden, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return "", false
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return "", false
		}
		if !accountLimit(w, r, limiter, "notifications", actor.ID, 120, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/notifications", clientIP(r, c), 240, time.Minute)
			if !limitResult(w, allowed, err) {
				return "", false
			}
		}
		return actor.ID, true
	}
	notificationPushRoutes(mux, service, sessions, c, authorize, fail)
	notificationPreferenceRoutes(mux, service, authorize, fail)
	notificationBroadcastRoutes(mux, service, authorize, fail)
	mux.HandleFunc("GET /api/v1/notifications", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, notifications.ErrInvalid)
			return
		}
		for key, values := range query {
			if (key != "limit" && key != "cursor") || len(values) != 1 {
				fail(w, notifications.ErrInvalid)
				return
			}
		}
		options := notifications.Options{Cursor: query.Get("cursor")}
		if query.Has("limit") {
			options.Limit, err = strconv.Atoi(query.Get("limit"))
			if err != nil {
				fail(w, notifications.ErrInvalid)
				return
			}
		}
		page, err := service.List(r.Context(), actor, options)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("GET /api/v1/notifications/unread-count", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		count, err := service.UnreadCount(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]int{"unreadCount": count}})
	})
	mux.HandleFunc("PUT /api/v1/notifications/{notificationId}/read", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, notifications.ErrInvalid)
			return
		}
		if _, ok := decodeJSON[struct{}](w, r); !ok {
			return
		}
		changed, err := service.MarkRead(r.Context(), actor, r.PathValue("notificationId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"changed": changed}})
	})
}

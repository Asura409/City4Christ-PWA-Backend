package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/events"
)

func eventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, events.ErrForbidden):
		writeError(w, 403, "forbidden", "You do not have permission for this event action.", nil)
	case errors.Is(err, events.ErrNotFound):
		writeError(w, 404, "event_not_found", "Event not found.", nil)
	case errors.Is(err, events.ErrFullCapacity):
		writeError(w, 409, "event_full", "The event has no remaining capacity for that RSVP.", nil)
	case errors.Is(err, events.ErrRegistrationClosed):
		writeError(w, 409, "registration_closed", "This event is not accepting RSVP changes.", nil)
	case errors.Is(err, events.ErrVersionConflict):
		writeError(w, 409, "version_conflict", "This event changed. Reload it before saving.", nil)
	case errors.Is(err, events.ErrConflict):
		writeError(w, 409, "event_conflict", "This event cannot be changed in its current state.", nil)
	case errors.Is(err, events.ErrInvalid):
		writeError(w, 422, "validation_failed", "Check the event and RSVP fields.", nil)
	default:
		writeError(w, 503, "unavailable", "Service unavailable.", nil)
	}
}

func eventRoutes(mux *http.ServeMux, service *events.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil {
			writeError(w, 503, "unavailable", "Event authentication is not configured.", nil)
			return "", false
		}
		if write && !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return "", false
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return "", false
		}
		if !accountLimit(w, r, limiter, "events", actor.ID, 60, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, e := limiter.Allow(r.Context(), "ip/events", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, e) {
				return "", false
			}
		}
		return actor.ID, true
	}
	viewer := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if authorization == "" {
			return "", true
		}
		return authorize(w, r, false)
	}
	if service == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q) > 1 || (len(q) > 0 && (len(q["groupId"]) != 1 || q.Get("groupId") == "")) {
			eventError(w, events.ErrInvalid)
			return
		}
		actor, ok := viewer(w, r)
		if !ok {
			return
		}
		var result []events.Event
		if q.Get("groupId") != "" {
			if actor == "" {
				actor, ok = authorize(w, r, false)
				if !ok {
					return
				}
			}
			result, err = service.GroupList(r.Context(), actor, q.Get("groupId"), false)
		} else {
			result, err = service.List(r.Context(), actor)
		}
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("GET /api/v1/events/{eventId}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		actor, ok := viewer(w, r)
		if !ok {
			return
		}
		result, err := service.Detail(r.Context(), actor, r.PathValue("eventId"))
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("GET /api/v1/events/{eventId}/calendar.ics", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		actor, ok := viewer(w, r)
		if !ok {
			return
		}
		calendar, err := service.Calendar(r.Context(), r.PathValue("eventId"), actor)
		if err != nil {
			eventError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="city4christ-event.ics"`)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(calendar)
	})
	mux.HandleFunc("GET /api/v1/events/{eventId}/coordinators", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		result, err := service.Coordinators(r.Context(), r.PathValue("eventId"), actor)
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": result})
	})
	mux.HandleFunc("PUT /api/v1/events/{eventId}/coordinators", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		input, ok := decodeJSON[struct {
			GroupID   string `json:"groupId"`
			AccountID string `json:"accountId"`
		}](w, r)
		if !ok {
			return
		}
		result, err := service.AssignCoordinator(r.Context(), actor, r.PathValue("eventId"), input.GroupID, input.AccountID)
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": result})
	})
	mux.HandleFunc("DELETE /api/v1/events/{eventId}/coordinators/{accountId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		if err := service.RemoveCoordinator(r.Context(), actor, r.PathValue("eventId"), r.PathValue("accountId")); err != nil {
			eventError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/admin/events", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		input, ok := decodeJSON[events.CreateInput](w, r)
		if !ok {
			return
		}
		result, err := service.Create(r.Context(), actor, input)
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": result})
	})
	mux.HandleFunc("GET /api/v1/admin/events", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q) > 1 || (len(q) > 0 && (len(q["groupId"]) != 1 || q.Get("groupId") == "")) {
			eventError(w, events.ErrInvalid)
			return
		}
		var result []events.Event
		if q.Get("groupId") != "" {
			result, err = service.GroupList(r.Context(), actor, q.Get("groupId"), true)
		} else {
			result, err = service.AdminList(r.Context(), actor)
		}
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("PATCH /api/v1/admin/events/{eventId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		input, ok := decodeJSON[events.UpdateInput](w, r)
		if !ok {
			return
		}
		result, err := service.Update(r.Context(), actor, r.PathValue("eventId"), input)
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("POST /api/v1/events/{eventId}/rsvp", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		input, ok := decodeJSON[struct {
			AttendeeCount int `json:"attendeeCount"`
		}](w, r)
		if !ok {
			return
		}
		result, err := service.RSVP(r.Context(), actor, r.PathValue("eventId"), input.AttendeeCount)
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("DELETE /api/v1/events/{eventId}/rsvp", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			eventError(w, events.ErrInvalid)
			return
		}
		changed, err := service.CancelRSVP(r.Context(), actor, r.PathValue("eventId"))
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]bool{"cancelled": changed}})
	})
}

package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/sermons"
)

func sermonMemberRoutes(mux *http.ServeMux, service *sermons.MemberService, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, sermons.ErrNotFound):
			writeError(w, http.StatusNotFound, "sermon_not_found", "Sermon not found.", nil)
		case errors.Is(err, sermons.ErrInvalidMemberInput):
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Check the sermon progress or note.", nil)
		default:
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Session authentication is not configured.", nil)
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
		if write && !accountLimit(w, r, limiter, "sermon-member-write", actor.ID, 30, time.Minute) {
			return "", false
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/me/sermons/{sermonId}/progress", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		progress, err := service.GetProgress(r.Context(), actor, r.PathValue("sermonId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": progress})
	})
	mux.HandleFunc("PUT /api/v1/me/sermons/{sermonId}/progress", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		input, ok := decodeJSON[struct {
			PositionSeconds *int `json:"positionSeconds"`
		}](w, r)
		if !ok {
			return
		}
		if input.PositionSeconds == nil {
			fail(w, sermons.ErrInvalidMemberInput)
			return
		}
		progress, err := service.SaveProgress(r.Context(), actor, r.PathValue("sermonId"), *input.PositionSeconds)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": progress})
	})
	mux.HandleFunc("GET /api/v1/me/sermons/{sermonId}/notes", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		note, err := service.GetNote(r.Context(), actor, r.PathValue("sermonId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": note})
	})
	mux.HandleFunc("PUT /api/v1/me/sermons/{sermonId}/notes", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		input, ok := decodeJSON[struct {
			Body string `json:"body"`
		}](w, r)
		if !ok {
			return
		}
		note, err := service.SaveNote(r.Context(), actor, r.PathValue("sermonId"), input.Body)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": note})
	})
	mux.HandleFunc("DELETE /api/v1/me/sermons/{sermonId}/notes", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if err := service.DeleteNote(r.Context(), actor, r.PathValue("sermonId")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

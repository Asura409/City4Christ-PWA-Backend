package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/prayer"
)

func prayerRoutes(mux *http.ServeMux, service *prayer.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	writeErrorFor := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, prayer.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission for this prayer request.", nil)
		case errors.Is(err, prayer.ErrNotFound):
			writeError(w, http.StatusNotFound, "prayer_not_found", "Prayer request not found.", nil)
		case errors.Is(err, prayer.ErrInvalid):
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Check the prayer request or pagination fields.", nil)
		case errors.Is(err, prayer.ErrConflict):
			writeError(w, http.StatusConflict, "state_conflict", "The prayer request cannot make that status change.", nil)
		default:
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Prayer services are not configured.", nil)
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
		if !accountLimit(w, r, limiter, "prayer", actor.ID, 60, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/prayer", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, err) {
				return "", false
			}
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/prayer/requests", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		for key, values := range q {
			if (key != "limit" && key != "cursor" && key != "status") || len(values) != 1 {
				writeErrorFor(w, prayer.ErrInvalid)
				return
			}
		}
		options := prayer.Options{Cursor: q.Get("cursor"), Status: q.Get("status")}
		if q.Has("limit") {
			options.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				writeErrorFor(w, prayer.ErrInvalid)
				return
			}
		}
		page, err := service.List(r.Context(), actor, options)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("POST /api/v1/prayer/requests", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		input, ok := decodeJSON[prayer.CreateInput](w, r)
		if !ok {
			return
		}
		item, err := service.Create(r.Context(), actor, input)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": item})
	})
	mux.HandleFunc("GET /api/v1/prayer/requests/{requestId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		item, err := service.Get(r.Context(), actor, r.PathValue("requestId"))
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": item})
	})
	mux.HandleFunc("PATCH /api/v1/prayer/requests/{requestId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		input, ok := decodeJSON[struct {
			Status string `json:"status"`
		}](w, r)
		if !ok {
			return
		}
		changed, err := service.SetStatus(r.Context(), actor, r.PathValue("requestId"), input.Status)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": prayer.StatusResult{Changed: changed}})
	})
	mux.HandleFunc("PUT /api/v1/prayer/requests/{requestId}/prayed", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		if _, ok := decodeJSON[struct{}](w, r); !ok {
			return
		}
		result, err := service.Pray(r.Context(), actor, r.PathValue("requestId"))
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": result})
	})
	mux.HandleFunc("POST /api/v1/prayer/requests/{requestId}/reports", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		input, ok := decodeJSON[prayer.ReportInput](w, r)
		if !ok {
			return
		}
		item, err := service.Report(r.Context(), actor, r.PathValue("requestId"), input)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": item})
	})
	mux.HandleFunc("GET /api/v1/prayer/reports", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		for key, values := range q {
			if (key != "limit" && key != "status") || len(values) != 1 {
				writeErrorFor(w, prayer.ErrInvalid)
				return
			}
		}
		options := prayer.Options{Status: q.Get("status")}
		if q.Has("limit") {
			options.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				writeErrorFor(w, prayer.ErrInvalid)
				return
			}
		}
		page, err := service.ListReports(r.Context(), actor, options)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": page.Items})
	})
	mux.HandleFunc("PATCH /api/v1/prayer/reports/{reportId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, prayer.ErrInvalid)
			return
		}
		input, ok := decodeJSON[prayer.ReviewInput](w, r)
		if !ok {
			return
		}
		changed, err := service.ReviewReport(r.Context(), actor, r.PathValue("reportId"), input)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": prayer.StatusResult{Changed: changed}})
	})
}

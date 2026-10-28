package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/posts"
)

func postModerationRoutes(mux *http.ServeMux, service *posts.ModerationService, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	writeErrorFor := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, posts.ErrModerationForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to review community reports.", nil)
		case errors.Is(err, posts.ErrReportNotFound):
			writeError(w, http.StatusNotFound, "report_not_found", "Content report or target not found.", nil)
		case errors.Is(err, posts.ErrReportInvalid):
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Check the report or review fields.", nil)
		case errors.Is(err, posts.ErrReportConflict):
			writeError(w, http.StatusConflict, "state_conflict", "This content was already reported or reviewed.", nil)
		default:
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
		}
	}
	mux.HandleFunc("POST /api/v1/content-reports", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "posts", true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrReportInvalid)
			return
		}
		input, ok := decodeJSON[posts.ReportInput](w, r)
		if !ok {
			return
		}
		item, err := service.Report(r.Context(), actor, input)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": item})
	})
	mux.HandleFunc("GET /api/v1/moderation/content-reports", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "moderation", false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeErrorFor(w, posts.ErrReportInvalid)
			return
		}
		for key, values := range q {
			if (key != "status" && key != "limit" && key != "cursor") || len(values) != 1 {
				writeErrorFor(w, posts.ErrReportInvalid)
				return
			}
		}
		options := posts.ReportOptions{Status: q.Get("status"), Cursor: q.Get("cursor")}
		if q.Has("limit") {
			options.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				writeErrorFor(w, posts.ErrReportInvalid)
				return
			}
		}
		page, err := service.ListReports(r.Context(), actor, options)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("POST /api/v1/moderation/content-reports/{reportId}/review", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "moderation", true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrReportInvalid)
			return
		}
		input, ok := decodeJSON[posts.ReviewInput](w, r)
		if !ok {
			return
		}
		if err := service.Review(r.Context(), actor, r.PathValue("reportId"), input); err != nil {
			writeErrorFor(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

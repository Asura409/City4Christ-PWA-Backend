package app

import (
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/visitors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func visitorError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, visitors.ErrInvalid):
		fields := map[string]string{}
		var v *visitors.ValidationError
		if errors.As(e, &v) {
			fields = v.Fields
		}
		writeError(w, 422, "validation_failed", "Check the visitor form fields and consent.", fields)
	case errors.Is(e, visitors.ErrForbidden):
		writeError(w, 403, "forbidden", "You do not have permission to follow up visitors.", nil)
	case errors.Is(e, visitors.ErrNotFound):
		writeError(w, 404, "visitor_not_found", "Visitor contact details are not available.", nil)
	case errors.Is(e, visitors.ErrConflict):
		writeError(w, 409, "visitor_conflict", "This submission or record changed. Reload before continuing.", nil)
	default:
		writeError(w, 503, "visitor_unavailable", "Visitor welcome services are unavailable.", nil)
	}
}
func visitorRoutes(mux *http.ServeMux, db *sql.DB, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	s := visitors.NewService(visitors.NewRepository(db), c.VisitorProtection, c.Now)
	enabled := c.VisitorIntakeEnabled && c.VisitorProtection != nil && db != nil && limiter != nil && validOrigin(c.AppOrigin)
	mux.HandleFunc("GET /api/v1/visitors/config", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			visitorError(w, visitors.ErrInvalid)
			return
		}
		if limiter != nil {
			ok, e := limiter.Allow(r.Context(), "visitor-config", clientIP(r, c), 60, time.Minute)
			if !limitResult(w, ok, e) {
				return
			}
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"enabled": enabled, "retentionDays": visitors.RetentionDays, "policyVersion": visitors.PolicyVersion, "consentText": visitors.ConsentText}})
	})
	mux.HandleFunc("POST /api/v1/visitors", func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			visitorError(w, visitors.ErrUnavailable)
			return
		}
		if !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			visitorError(w, visitors.ErrInvalid)
			return
		}
		for _, budget := range []struct {
			key    string
			n      int
			window time.Duration
		}{{"visitor-hour", 60, time.Hour}, {"visitor-day", 300, 24 * time.Hour}} {
			ok, e := limiter.Allow(r.Context(), budget.key, clientIP(r, c), budget.n, budget.window)
			if e != nil {
				visitorError(w, visitors.ErrUnavailable)
				return
			}
			if !ok {
				w.Header().Set("Retry-After", "3600")
				writeError(w, 429, "rate_limited", "Too many welcome forms. Please try again later.", nil)
				return
			}
		}
		in, ok := decodeJSON[visitors.Submission](w, r)
		if !ok {
			return
		}
		if e := s.Submit(r.Context(), in); e != nil {
			visitorError(w, e)
			return
		}
		writeJSON(w, 202, map[string]any{"data": map[string]string{"message": "Your welcome form has been received."}})
	})
	visitorFollowUpRoutes(mux, s, sessions, limiter, c)
}
func visitorFollowUpRoutes(mux *http.ServeMux, s *visitors.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	authn := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		return authorizePostMember(w, r, sessions, limiter, c, "visitors-follow-up", write)
	}
	mux.HandleFunc("GET /api/v1/admin/visitors", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authn(w, r, false)
		if !ok {
			return
		}
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			visitorError(w, visitors.ErrInvalid)
			return
		}
		for k, v := range q {
			if len(v) != 1 || (k != "status" && k != "cursor" && k != "limit") {
				visitorError(w, visitors.ErrInvalid)
				return
			}
		}
		o := visitors.Options{Cursor: q.Get("cursor"), Status: q.Get("status")}
		if q.Has("limit") {
			o.Limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil || o.Limit < 1 {
				visitorError(w, visitors.ErrInvalid)
				return
			}
		}
		p, e := s.List(r.Context(), actor, o)
		if e != nil {
			visitorError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p.Items, "meta": map[string]string{"nextCursor": p.NextCursor}})
	})
	mux.HandleFunc("GET /api/v1/admin/visitors/{visitorId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authn(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			visitorError(w, visitors.ErrInvalid)
			return
		}
		d, e := s.Detail(r.Context(), actor, r.PathValue("visitorId"))
		if e != nil {
			visitorError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"data": d})
	})
	mux.HandleFunc("PATCH /api/v1/admin/visitors/{visitorId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authn(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			visitorError(w, visitors.ErrInvalid)
			return
		}
		in, ok := decodeJSON[visitors.Update](w, r)
		if !ok {
			return
		}
		v, e := s.Update(r.Context(), actor, r.PathValue("visitorId"), in)
		if e != nil {
			visitorError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"data": v})
	})
	mux.HandleFunc("POST /api/v1/admin/visitors/{visitorId}/assignment", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authn(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			visitorError(w, visitors.ErrInvalid)
			return
		}
		in, ok := decodeJSON[visitors.AssignmentInput](w, r)
		if !ok {
			return
		}
		v, e := s.Assign(r.Context(), actor, r.PathValue("visitorId"), in)
		if e != nil {
			visitorError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"data": v})
	})
}

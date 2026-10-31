package app

import (
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/devotionals"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type devotionalHTTP struct {
	service  *devotionals.Service
	sessions *auth.SessionService
	limiter  *auth.RateLimiter
	config   Config
}

func devotionalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, devotionals.ErrForbidden):
		writeError(w, 403, "forbidden", "Devotional editor permission is required.", nil)
	case errors.Is(err, devotionals.ErrNotFound):
		writeError(w, 404, "devotional_not_found", "Devotional not found.", nil)
	case errors.Is(err, devotionals.ErrConflict):
		writeError(w, 409, "version_conflict", "The devotional changed. Reload its latest version before editing.", nil)
	case errors.Is(err, devotionals.ErrInvalid):
		var v *devotionals.ValidationError
		fields := map[string]string{}
		if errors.As(err, &v) {
			fields = v.Fields
		}
		writeError(w, 422, "validation_failed", "Check the indicated devotional fields.", fields)
	default:
		writeError(w, 503, "unavailable", "Devotionals are unavailable.", nil)
	}
}
func (d devotionalHTTP) ipLimit(w http.ResponseWriter, r *http.Request, scope string) bool {
	if d.limiter == nil {
		return true
	}
	ok, err := d.limiter.Allow(r.Context(), "devotional-ip/"+scope, clientIP(r, d.config), 120, time.Minute)
	return limitResult(w, ok, err)
}
func (d devotionalHTTP) authorize(w http.ResponseWriter, r *http.Request) (string, bool) {
	if d.sessions == nil || !validOrigin(d.config.AppOrigin) {
		writeError(w, 503, "unavailable", "Editor authentication is unavailable.", nil)
		return "", false
	}
	write := r.Method != "GET"
	if write && !originAllowed(d.config, r.Header.Get("Origin")) {
		writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
		return "", false
	}
	actor, err := d.sessions.Authenticate(r.Context(), requestAccess(r))
	if err != nil {
		sessionError(w, err)
		return "", false
	}
	if err = d.service.CanEdit(r.Context(), actor.ID); err != nil {
		devotionalError(w, err)
		return "", false
	}
	scope := "read"
	limit := 60
	if write {
		scope = "write"
		limit = 30
	}
	if !accountLimit(w, r, d.limiter, "devotional-"+scope, actor.ID, limit, time.Minute) || !d.ipLimit(w, r, scope) {
		return "", false
	}
	return actor.ID, true
}
func devotionalOptions(w http.ResponseWriter, r *http.Request) (devotionals.Options, bool) {
	o := devotionals.Options{}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		devotionalError(w, devotionals.ErrInvalid)
		return o, false
	}
	for k, v := range q {
		if len(v) != 1 || (k != "limit" && k != "cursor") {
			devotionalError(w, devotionals.ErrInvalid)
			return o, false
		}
	}
	o.Cursor = q.Get("cursor")
	if q.Has("limit") {
		o.Limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || o.Limit < 1 || o.Limit > 100 {
			devotionalError(w, devotionals.ErrInvalid)
			return o, false
		}
	}
	return o, true
}
func devotionalNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		devotionalError(w, devotionals.ErrInvalid)
		return false
	}
	return true
}
func devotionalPage[T any](w http.ResponseWriter, p devotionals.Page[T]) {
	writeJSON(w, 200, map[string]any{"data": p.Items, "meta": map[string]string{"nextCursor": p.NextCursor}})
}

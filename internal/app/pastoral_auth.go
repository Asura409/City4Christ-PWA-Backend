package app

import (
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type pastoralHTTP struct {
	service  *pastoral.Service
	sessions *auth.SessionService
	limiter  *auth.RateLimiter
	config   Config
}

func (p pastoralHTTP) authorize(w http.ResponseWriter, r *http.Request) (string, bool) {
	if p.sessions == nil || !validOrigin(p.config.AppOrigin) {
		writeError(w, 503, "unavailable", "Pastoral services are not configured.", nil)
		return "", false
	}
	write := r.Method != "GET"
	if write && !originAllowed(p.config, r.Header.Get("Origin")) {
		writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
		return "", false
	}
	actor, err := p.sessions.Authenticate(r.Context(), requestAccess(r))
	if err != nil {
		sessionError(w, err)
		return "", false
	}
	scope := "pastoral-read"
	limit := 60
	if write {
		scope = "pastoral-write"
		limit = 30
	}
	if !accountLimit(w, r, p.limiter, scope, actor.ID, limit, time.Minute) {
		return "", false
	}
	if p.limiter != nil {
		allowed, e := p.limiter.Allow(r.Context(), "ip/"+scope, clientIP(r, p.config), 120, time.Minute)
		if !limitResult(w, allowed, e) {
			return "", false
		}
	}
	return actor.ID, true
}
func pastoralError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pastoral.ErrInvalid):
		writeError(w, 422, "validation_failed", "Check appointment fields, time range and pagination.", nil)
	case errors.Is(err, pastoral.ErrForbidden):
		writeError(w, 403, "forbidden", "This pastoral action is not permitted.", nil)
	case errors.Is(err, pastoral.ErrNotFound):
		writeError(w, 404, "pastoral_not_found", "Pastoral resource not found.", nil)
	case errors.Is(err, pastoral.ErrConflict):
		writeError(w, 409, "appointment_conflict", "This slot or appointment has changed. Reload before retrying.", nil)
	default:
		sessionError(w, err)
	}
}
func pastoralOptions(w http.ResponseWriter, r *http.Request, withRange bool) (pastoral.Options, url.Values, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		pastoralError(w, pastoral.ErrInvalid)
		return pastoral.Options{}, nil, false
	}
	for key, values := range q {
		if len(values) != 1 || (key != "limit" && key != "cursor" && (!withRange || (key != "from" && key != "to"))) {
			pastoralError(w, pastoral.ErrInvalid)
			return pastoral.Options{}, nil, false
		}
	}
	o := pastoral.Options{Cursor: q.Get("cursor")}
	if q.Has("limit") {
		n, e := strconv.Atoi(q.Get("limit"))
		if e != nil || n < 1 || n > 100 {
			pastoralError(w, pastoral.ErrInvalid)
			return o, nil, false
		}
		o.Limit = n
	}
	return o, q, true
}
func pastoralNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		pastoralError(w, pastoral.ErrInvalid)
		return false
	}
	return true
}
func pastoralPage[T any](w http.ResponseWriter, page pastoral.Page[T]) {
	writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
}

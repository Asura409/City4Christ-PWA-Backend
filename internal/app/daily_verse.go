package app

import (
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/devotionals"
	"net/http"
)

func dailyVerseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, devotionals.ErrVerseDateConflict):
		writeError(w, 409, "verse_date_conflict", "A verse is already published for that date. Withdraw it or choose another date.", map[string]string{"displayDate": "Only one verse can be published per church date."})
	case errors.Is(err, devotionals.ErrNotFound):
		writeError(w, 404, "daily_verse_not_found", "Daily verse not found.", nil)
	case errors.Is(err, devotionals.ErrConflict):
		writeError(w, 409, "version_conflict", "The verse changed. Reload its latest version before editing.", nil)
	default:
		devotionalError(w, err)
	}
}
func dailyVerseRoutes(mux *http.ServeMux, d devotionalHTTP) {
	s := d.service
	mux.HandleFunc("GET /api/v1/daily-verse", func(w http.ResponseWriter, r *http.Request) {
		if !d.ipLimit(w, r, "public") || !devotionalNoQuery(w, r) {
			return
		}
		result, err := s.TodayVerse(r.Context())
		if err != nil {
			dailyVerseError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("POST /api/v1/admin/daily-verses", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok || !devotionalNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[devotionals.VerseContent](w, r)
		if !ok {
			return
		}
		v, err := s.CreateVerse(r.Context(), actor, in)
		if err != nil {
			dailyVerseError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/admin/daily-verses/"+v.ID)
		writeJSON(w, 201, map[string]any{"data": v})
	})
	mux.HandleFunc("GET /api/v1/admin/daily-verses", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok {
			return
		}
		o, ok := devotionalOptions(w, r)
		if !ok {
			return
		}
		page, err := s.ListVersesEditorial(r.Context(), actor, o)
		if err != nil {
			dailyVerseError(w, err)
			return
		}
		devotionalPage(w, page)
	})
	mux.HandleFunc("GET /api/v1/admin/daily-verses/{verseId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok || !devotionalNoQuery(w, r) {
			return
		}
		v, err := s.GetVerseEditorial(r.Context(), actor, r.PathValue("verseId"))
		if err != nil {
			dailyVerseError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": v})
	})
	mux.HandleFunc("PATCH /api/v1/admin/daily-verses/{verseId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok || !devotionalNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[devotionals.VerseUpdate](w, r)
		if !ok {
			return
		}
		v, err := s.UpdateVerse(r.Context(), actor, r.PathValue("verseId"), in)
		if err != nil {
			dailyVerseError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": v})
	})
}

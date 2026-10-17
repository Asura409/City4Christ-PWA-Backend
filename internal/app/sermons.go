package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/sermons"
)

func sermonRoutes(mux *http.ServeMux, service *sermons.Service) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, sermons.ErrNotFound):
			writeError(w, 404, "sermon_not_found", "Sermon not found.", nil)
		case errors.Is(err, sermons.ErrInvalidOptions):
			writeError(w, 422, "validation_failed", "Check the catalog filters, limit and cursor.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	mux.HandleFunc("GET /api/v1/sermons", func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, sermons.ErrInvalidOptions)
			return
		}
		for key, values := range q {
			switch key {
			case "query", "speaker", "series", "topic", "limit", "cursor":
			default:
				fail(w, sermons.ErrInvalidOptions)
				return
			}
			if len(values) != 1 {
				fail(w, sermons.ErrInvalidOptions)
				return
			}
		}
		options := sermons.Options{Query: q.Get("query"), Speaker: q.Get("speaker"), Series: q.Get("series"), Topic: q.Get("topic"), Cursor: q.Get("cursor")}
		if q.Has("limit") {
			options.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil || options.Limit < 1 || options.Limit > 100 {
				fail(w, sermons.ErrInvalidOptions)
				return
			}
		}
		page, err := service.List(r.Context(), options)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("GET /api/v1/sermons/{sermonId}", func(w http.ResponseWriter, r *http.Request) {
		sermon, err := service.Get(r.Context(), r.PathValue("sermonId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": sermon})
	})
}

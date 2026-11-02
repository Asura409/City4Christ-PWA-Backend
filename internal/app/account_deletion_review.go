package app

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/accountdeletion"
)

func (h deletionHTTP) reviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/account-deletion-requests", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.authorize(w, r, true)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			deletionError(w, accountdeletion.ErrInvalid)
			return
		}
		for name, values := range q {
			if (name != "status" && name != "cursor" && name != "limit") || len(values) != 1 {
				deletionError(w, accountdeletion.ErrInvalid)
				return
			}
		}
		limit := 20
		if values, exists := q["limit"]; exists {
			limit, err = strconv.Atoi(values[0])
			if err != nil || limit < 1 || limit > 100 {
				deletionError(w, accountdeletion.ErrInvalid)
				return
			}
		}
		page, err := h.service.List(r.Context(), actor, q.Get("status"), q.Get("cursor"), limit, h.guard(r, true))
		if err != nil {
			deletionError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]any{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("GET /api/v1/admin/account-deletion-requests/{requestId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.authorize(w, r, true)
		if !ok || !deletionNoQuery(w, r) {
			return
		}
		data, err := h.service.Detail(r.Context(), actor, r.PathValue("requestId"), h.guard(r, true))
		if err != nil {
			deletionError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})
	mux.HandleFunc("PATCH /api/v1/admin/account-deletion-requests/{requestId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.authorize(w, r, true)
		if !ok || !deletionNoQuery(w, r) {
			return
		}
		input, ok := decodeJSON[struct {
			Status  string `json:"status"`
			Version int    `json:"version"`
		}](w, r)
		if !ok {
			return
		}
		if input.Status != "reviewing" || input.Version < 1 {
			deletionError(w, accountdeletion.ErrInvalid)
			return
		}
		data, err := h.service.Review(r.Context(), actor, r.PathValue("requestId"), input.Version, h.guard(r, true))
		if err != nil {
			deletionError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})
	mux.HandleFunc("POST /api/v1/admin/account-deletion-requests/{requestId}/restore", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.authorize(w, r, true)
		if !ok || !deletionNoQuery(w, r) {
			return
		}
		input, ok := decodeJSON[struct {
			Version int    `json:"version"`
			Reason  string `json:"reason"`
		}](w, r)
		if !ok {
			return
		}
		data, err := h.service.Restore(r.Context(), actor, r.PathValue("requestId"), input.Version, input.Reason, h.guard(r, true))
		if err != nil {
			deletionError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})
}

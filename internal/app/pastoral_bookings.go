package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"net/http"
)

func pastoralBookingRoutes(mux *http.ServeMux, p pastoralHTTP) {
	mux.HandleFunc("POST /api/v1/pastor-bookings", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[struct {
			SlotID string `json:"slotId"`
		}](w, r)
		if !ok {
			return
		}
		b, err := p.service.Request(r.Context(), actor, in.SlotID)
		if err != nil {
			pastoralError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": b})
	})
	list := func(assigned bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actor, ok := p.authorize(w, r)
			if !ok {
				return
			}
			o, _, ok := pastoralOptions(w, r, false)
			if !ok {
				return
			}
			page, err := p.service.Bookings(r.Context(), actor, assigned, o)
			if err != nil {
				pastoralError(w, err)
				return
			}
			pastoralPage(w, page)
		}
	}
	mux.HandleFunc("GET /api/v1/me/pastor-bookings", list(false))
	mux.HandleFunc("GET /api/v1/me/pastoral/bookings", list(true))
	mux.HandleFunc("DELETE /api/v1/pastor-bookings/{bookingId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			pastoralError(w, pastoral.ErrInvalid)
			return
		}
		b, err := p.service.Cancel(r.Context(), actor, r.PathValue("bookingId"))
		if err != nil {
			pastoralError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": b})
	})
	mux.HandleFunc("PATCH /api/v1/me/pastoral/bookings/{bookingId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[pastoral.Decision](w, r)
		if !ok {
			return
		}
		b, err := p.service.Decide(r.Context(), actor, r.PathValue("bookingId"), in)
		if err != nil {
			pastoralError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": b})
	})
}

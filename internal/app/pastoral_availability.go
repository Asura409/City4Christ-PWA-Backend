package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"net/http"
	"time"
)

func pastoralAvailabilityRoutes(mux *http.ServeMux, p pastoralHTTP) {
	list := func(own bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actor, ok := p.authorize(w, r)
			if !ok {
				return
			}
			o, q, ok := pastoralOptions(w, r, true)
			if !ok {
				return
			}
			now := time.Now().UTC()
			if p.config.Now != nil {
				now = p.config.Now().UTC()
			}
			from, to := now, now.Add(30*24*time.Hour)
			var err error
			if q.Has("from") {
				from, err = time.Parse(time.RFC3339Nano, q.Get("from"))
				if err != nil {
					pastoralError(w, pastoral.ErrInvalid)
					return
				}
			}
			if q.Has("to") {
				to, err = time.Parse(time.RFC3339Nano, q.Get("to"))
				if err != nil {
					pastoralError(w, pastoral.ErrInvalid)
					return
				}
			}
			var page pastoral.Page[pastoral.Slot]
			if own {
				page, err = p.service.OwnSlots(r.Context(), actor, from, to, o)
			} else {
				page, err = p.service.Availability(r.Context(), r.PathValue("pastorId"), from, to, o)
			}
			if err != nil {
				pastoralError(w, err)
				return
			}
			pastoralPage(w, page)
		}
	}
	mux.HandleFunc("GET /api/v1/pastors/{pastorId}/availability", list(false))
	mux.HandleFunc("GET /api/v1/me/pastoral/availability", list(true))
	mux.HandleFunc("POST /api/v1/me/pastoral/availability", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[pastoral.SlotInput](w, r)
		if !ok {
			return
		}
		slot, err := p.service.CreateSlot(r.Context(), actor, in)
		if err != nil {
			pastoralError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": slot})
	})
	mux.HandleFunc("DELETE /api/v1/me/pastoral/availability/{slotId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			pastoralError(w, pastoral.ErrInvalid)
			return
		}
		if err := p.service.WithdrawSlot(r.Context(), actor, r.PathValue("slotId")); err != nil {
			pastoralError(w, err)
			return
		}
		w.WriteHeader(204)
	})
}

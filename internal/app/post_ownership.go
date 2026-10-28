package app

import (
	"errors"
	"net/http"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/posts"
)

func postOwnershipRoutes(mux *http.ServeMux, service *posts.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	writeErrorFor := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, posts.ErrNotFound):
			writeError(w, http.StatusNotFound, "post_not_found", "Post not found.", nil)
		case errors.Is(err, posts.ErrInvalid):
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Check the post fields and attachments.", nil)
		default:
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
		}
	}
	mux.HandleFunc("PUT /api/v1/posts/{postId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "posts", true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrInvalid)
			return
		}
		input, ok := decodeJSON[posts.UpdateInput](w, r)
		if !ok {
			return
		}
		post, err := service.Update(r.Context(), actor, r.PathValue("postId"), input)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": post})
	})
	mux.HandleFunc("DELETE /api/v1/posts/{postId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "posts", true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrInvalid)
			return
		}
		if err := service.Delete(r.Context(), actor, r.PathValue("postId")); err != nil {
			writeErrorFor(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/posts"
)

func postCommentRoutes(mux *http.ServeMux, service *posts.CommentService, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	writeErrorFor := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, posts.ErrCommentNotFound):
			writeError(w, 404, "comment_not_found", "Post or comment not found.", nil)
		case errors.Is(err, posts.ErrCommentInvalid):
			writeError(w, 422, "validation_failed", "Check the comment or pagination fields.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	mux.HandleFunc("GET /api/v1/posts/{postId}/comments", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "post-comments", false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeErrorFor(w, posts.ErrCommentInvalid)
			return
		}
		for key, values := range q {
			if (key != "limit" && key != "cursor") || len(values) != 1 {
				writeErrorFor(w, posts.ErrCommentInvalid)
				return
			}
		}
		options := posts.CommentOptions{Cursor: q.Get("cursor")}
		if q.Has("limit") {
			options.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil || options.Limit < 1 || options.Limit > 100 {
				writeErrorFor(w, posts.ErrCommentInvalid)
				return
			}
		}
		page, err := service.List(r.Context(), actor, r.PathValue("postId"), options)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("POST /api/v1/posts/{postId}/comments", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "post-comments", true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrCommentInvalid)
			return
		}
		input, ok := decodeJSON[posts.CommentInput](w, r)
		if !ok {
			return
		}
		comment, err := service.Create(r.Context(), actor, r.PathValue("postId"), input)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": comment})
	})
	mux.HandleFunc("DELETE /api/v1/posts/{postId}/comments/{commentId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorizePostMember(w, r, sessions, limiter, c, "post-comments", true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrCommentInvalid)
			return
		}
		if err := service.Delete(r.Context(), actor, r.PathValue("postId"), r.PathValue("commentId")); err != nil {
			writeErrorFor(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

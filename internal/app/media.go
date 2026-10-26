package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

func mediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrInvalidMediaUpload):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Check the media purpose, content type and size.", nil)
	case errors.Is(err, media.ErrUploadMismatch):
		writeError(w, http.StatusUnprocessableEntity, "upload_mismatch", "The uploaded object does not match its reservation.", nil)
	case errors.Is(err, media.ErrUploadNotComplete):
		writeError(w, http.StatusConflict, "upload_not_complete", "The uploaded object is not available yet.", nil)
	case errors.Is(err, media.ErrUploadExpired):
		writeError(w, http.StatusGone, "upload_expired", "The upload reservation expired.", nil)
	case errors.Is(err, media.ErrUploadNotFound):
		writeError(w, http.StatusNotFound, "upload_not_found", "Upload not found.", nil)
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Media uploads are unavailable.", nil)
	}
}

func mediaRoutes(mux *http.ServeMux, service *media.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Media authentication is not configured.", nil)
			return "", false
		}
		if write && !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, http.StatusForbidden, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return "", false
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return "", false
		}
		if !accountLimit(w, r, limiter, "media", actor.ID, 20, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/media", clientIP(r, c), 40, time.Minute)
			if !limitResult(w, allowed, err) {
				return "", false
			}
		}
		return actor.ID, true
	}

	mux.HandleFunc("POST /api/v1/uploads/presign", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			mediaError(w, media.ErrInvalidMediaUpload)
			return
		}
		input, ok := decodeJSON[media.PresignInput](w, r)
		if !ok {
			return
		}
		result, err := service.Presign(r.Context(), actor, input)
		if err != nil {
			mediaError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": result})
	})

	mux.HandleFunc("POST /api/v1/uploads/{uploadId}/complete", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			mediaError(w, media.ErrInvalidMediaUpload)
			return
		}
		if _, ok := decodeJSON[struct{}](w, r); !ok {
			return
		}
		upload, err := service.Complete(r.Context(), actor, r.PathValue("uploadId"))
		if err != nil {
			mediaError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"data": mediaUploadData(upload)})
	})

	mux.HandleFunc("GET /api/v1/uploads/{uploadId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			mediaError(w, media.ErrInvalidMediaUpload)
			return
		}
		upload, err := service.Status(r.Context(), actor, r.PathValue("uploadId"))
		if err != nil {
			mediaError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": mediaUploadData(upload)})
	})
}

func mediaUploadData(upload media.Upload) map[string]any {
	return map[string]any{
		"id": upload.ID, "purpose": upload.Purpose, "status": upload.Status,
		"contentType": upload.DeclaredContentType, "size": upload.DeclaredSize,
		"createdAt": upload.CreatedAt, "expiresAt": upload.ExpiresAt,
	}
}

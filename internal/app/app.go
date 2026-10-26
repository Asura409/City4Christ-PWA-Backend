// Package app owns the global HTTP router and dependency wiring.
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/events"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/groups"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/notifications"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/posts"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/prayer"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/profiles"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/sermons"
	"github.com/google/uuid"
)

type Config struct {
	RegistrationEnabled bool
	VerificationKey     []byte
	Now                 func() time.Time
	AppOrigin           string
	AllowedOrigins      []string
	CrossSiteCookies    bool
	TrustedProxyCIDRs   []string
	GoogleProvider      auth.GoogleProvider
	MediaStorage        media.UploadStorage
}

func New(db *sql.DB, config Config) http.Handler {
	mux := http.NewServeMux()
	documentationRoutes(mux)
	var downloadSigner media.DownloadSigner
	if signer, ok := config.MediaStorage.(media.DownloadSigner); ok {
		downloadSigner = signer
	}
	catalog := sermons.NewService(sermons.NewRepository(db), config.Now, downloadSigner)
	sermonRoutes(mux, catalog)
	verification, verificationErr := auth.NewVerificationService(db, auth.VerificationConfig{Key: config.VerificationKey, Now: config.Now})
	var service *auth.RegistrationService
	var sessions *auth.SessionService
	var limiter *auth.RateLimiter
	// Invalid/missing auth secrets leave health routes usable and auth unavailable.
	if verificationErr == nil {
		service = auth.NewRegistrationService(auth.NewPasswordHasher(), verification)
		sessions, _ = auth.NewSessionService(db, auth.SessionConfig{Key: config.VerificationKey, Now: config.Now})
		limiter = auth.NewRateLimiter(db, config.VerificationKey, config.Now)
	}
	mux.HandleFunc("POST /api/v1/auth/register", registrationHandler(service, config.RegistrationEnabled, limiter))
	mux.HandleFunc("POST /api/v1/auth/verification/verify", verificationHandler(verification, limiter))
	mux.HandleFunc("POST /api/v1/auth/verification/resend", resendHandler(verification, limiter))
	var recovery *auth.PasswordRecoveryService
	if verificationErr == nil {
		recovery = auth.NewPasswordRecoveryService(db, verification)
	}
	passwordRecoveryRoutes(mux, recovery, limiter, config)
	sessionRoutes(mux, sessions, limiter, config)
	profileService := profiles.NewService(profiles.NewRepository(db))
	profileRoutes(mux, profileService, sessions, limiter, config)
	directoryRoutes(mux, profileService, sessions, limiter, config)
	postRoutes(mux, posts.NewService(posts.NewRepository(db), downloadSigner), sessions, limiter, config)
	eventRoutes(mux, events.NewService(events.NewRepository(db), config.Now), sessions, limiter, config)
	sermonPublishingRoutes(mux, catalog, sessions, limiter, config)
	groupRoutes(mux, groups.NewService(groups.NewRepository(db), config.Now), sessions, limiter, config)
	prayerRoutes(mux, prayer.NewService(prayer.NewRepository(db)), sessions, limiter, config)
	directMessagingRoutes(mux, messaging.NewService(messaging.NewRepository(db), config.Now), sessions, limiter, config)
	notificationRoutes(mux, notifications.NewService(notifications.NewRepository(db), config.Now), sessions, limiter, config)
	mediaRoutes(mux, media.NewService(media.NewRepository(db), config.MediaStorage, config.Now), sessions, limiter, config)
	accessService := access.NewService(db)
	accessRoutes(mux, accessService, sessions, limiter, config)
	churchRoleRoutes(mux, accessService, sessions, limiter, config)
	currentAccessRoutes(mux, accessService, sessions, limiter, config)
	var google *auth.GoogleService
	if sessions != nil && config.GoogleProvider != nil {
		google = auth.NewGoogleService(db, sessions, config.GoogleProvider, config.RegistrationEnabled, config.Now)
	}
	googleRoutes(mux, google, config)
	reauthenticationRoutes(mux, sessions, google, limiter, config)
	mfa, _ := auth.NewMFAService(db, sessions, config.VerificationKey)
	mfaRoutes(mux, mfa, sessions, limiter, config)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Service unavailable.", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
	})
	browser := browserPolicy(mux, config, limiter)
	withRequestID := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate server-controlled correlation IDs; never reflect arbitrary input.
		w.Header().Set("X-Request-Id", uuid.NewString())
		browser.ServeHTTP(w, r)
	})
	return requestLogging(slog.Default(), withRequestID)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

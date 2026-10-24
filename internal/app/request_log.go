package app

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorded := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorded, r)
		status := recorded.status
		if status == 0 {
			status = http.StatusOK
		}
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			return
		}
		logger.Info("HTTP request completed",
			slog.String("request_id", w.Header().Get("X-Request-Id")),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
	})
}

package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLoggingCapturesSafeRequestSummary(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := requestLogging(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "request-correlation-id")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_credentials"}`)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login?password=query-secret", strings.NewReader(`{"email":"user@example.test","password":"body-secret"}`))
	request.Header.Set("Authorization", "Bearer header-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("log is not JSON: %v (%s)", err, logs.String())
	}
	for field, want := range map[string]any{
		"request_id": "request-correlation-id",
		"method":     http.MethodPost,
		"path":       "/api/v1/auth/login",
		"status":     float64(http.StatusUnauthorized),
	} {
		if entry[field] != want {
			t.Errorf("log field %q = %#v, want %#v", field, entry[field], want)
		}
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Error("log is missing duration_ms")
	}
	for _, secret := range []string{"query-secret", "body-secret", "header-secret", "user@example.test"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("request log leaked sensitive value %q", secret)
		}
	}
}

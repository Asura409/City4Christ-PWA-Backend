package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestHealthThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	app := New(db, Config{})
	for _, path := range []string{"/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data.Status != "ok" {
				t.Fatalf("unexpected response: %s", response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatal("missing JSON content type")
			}
		})
	}
	// Dependency failure must affect readiness but not process liveness.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/health/live", 200}, {"/health/ready", 503}} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if response.Code != tc.status {
			t.Fatalf("%s status=%d", tc.path, response.Code)
		}
		if tc.status == 503 {
			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "unavailable" || body.Error.Message != "Service unavailable." {
				t.Fatalf("unsafe error: %s", response.Body.String())
			}
		}
	}
}

func TestReadinessRespectsCanceledRequest(t *testing.T) {
	db := testpg.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	New(db, Config{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil).WithContext(ctx))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.Code)
	}
}

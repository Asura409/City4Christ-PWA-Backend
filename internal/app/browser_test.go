package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserPreflightAllowsPUT(t *testing.T) {
	handler := browserPolicy(http.NotFoundHandler(), Config{AppOrigin: "https://app.example"}, nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/prayer/requests/id/prayed", nil)
	req.Header.Set("Origin", "https://app.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent || !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("PUT preflight status=%d methods=%q", response.Code, response.Header().Get("Access-Control-Allow-Methods"))
	}
}

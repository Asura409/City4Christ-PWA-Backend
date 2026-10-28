package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPostOwnershipRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("p", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Member','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	authorID, authorToken := account("post-owner-route@example.invalid")
	_, otherToken := account("post-other-route@example.invalid")
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	created := request(http.MethodPost, "/api/v1/posts", authorToken, `{"body":"Before edit"}`, "https://api.example")
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var result struct {
		Data struct {
			ID       string `json:"id"`
			AuthorID string `json:"authorId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.AuthorID != authorID {
		t.Fatalf("created post author=%q want %q", result.Data.AuthorID, authorID)
	}
	path := "/api/v1/posts/" + result.Data.ID
	if response := request(http.MethodPut, path, otherToken, `{"body":"Not my post"}`, "https://api.example"); response.Code != http.StatusNotFound {
		t.Fatalf("other member edit: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPut, path, authorToken, `{"body":"After edit"}`, "https://api.example"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"body":"After edit"`) {
		t.Fatalf("owner edit: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPut, path, authorToken, `{"body":"No origin"}`, ""); response.Code != http.StatusForbidden {
		t.Fatalf("edit without Origin: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodDelete, path, otherToken, "", "https://api.example"); response.Code != http.StatusNotFound {
		t.Fatalf("other member delete: %d %s", response.Code, response.Body.String())
	}
	commentsPath := path + "/comments"
	if response := request(http.MethodPost, commentsPath, otherToken, `{"body":"A comment on the post"}`, "https://api.example"); response.Code != http.StatusCreated {
		t.Fatalf("create comment before deletion: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodDelete, path, authorToken, "", ""); response.Code != http.StatusForbidden {
		t.Fatalf("delete without Origin: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodDelete, path, authorToken, "", "https://api.example"); response.Code != http.StatusNoContent {
		t.Fatalf("owner delete: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodGet, "/api/v1/posts", otherToken, "", ""); response.Code != http.StatusOK || strings.Contains(response.Body.String(), "After edit") {
		t.Fatalf("deleted post remains visible: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodGet, commentsPath, otherToken, "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("deleted post comments remain available: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPut, path+"/reaction", otherToken, `{"reaction":"amen"}`, "https://api.example"); response.Code != http.StatusNotFound {
		t.Fatalf("deleted post still accepts reactions: %d %s", response.Code, response.Body.String())
	}
}

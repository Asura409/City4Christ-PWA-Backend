package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPostsThroughGlobalApp(t *testing.T) {
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
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Post','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	_, memberToken := account("posts@example.invalid")
	_, otherToken := account("reader@example.invalid")
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example"})
	request := func(method, path, token, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	created := request("POST", "/api/v1/posts", memberToken, `{"body":"Welcome to Sunday service"}`, "https://api.example")
	if created.Code != 201 {
		t.Fatalf("create post: %d %s", created.Code, created.Body.String())
	}
	var post struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &post); err != nil {
		t.Fatal(err)
	}
	commentPath := "/api/v1/posts/" + post.Data.ID + "/comments"
	comment := request("POST", commentPath, memberToken, `{"body":"Praying with you."}`, "https://api.example")
	if comment.Code != 201 || !strings.Contains(comment.Body.String(), `"body":"Praying with you."`) {
		t.Fatalf("create post comment: %d %s", comment.Code, comment.Body.String())
	}
	var createdComment struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(comment.Body.Bytes(), &createdComment); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", commentPath, otherToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Praying with you.") {
		t.Fatalf("member reads church-wide comments: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", commentPath, "", "", ""); w.Code != 401 {
		t.Fatalf("anonymous member read comments: %d %s", w.Code, w.Body.String())
	}
	if w := request("DELETE", commentPath+"/"+createdComment.Data.ID, otherToken, "", "https://api.example"); w.Code != 404 {
		t.Fatalf("member deleted another author’s comment: %d %s", w.Code, w.Body.String())
	}
	if w := request("DELETE", commentPath+"/"+createdComment.Data.ID, memberToken, "", "https://api.example"); w.Code != 204 {
		t.Fatalf("comment owner deletes comment: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", commentPath, memberToken, `{"body":"No origin"}`, ""); w.Code != 403 {
		t.Fatalf("comment write without origin: %d %s", w.Code, w.Body.String())
	}
	if w := request("PUT", "/api/v1/posts/"+post.Data.ID+"/reaction", memberToken, `{"reaction":"amen"}`, "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"amenCount":1`) || !strings.Contains(w.Body.String(), `"reacted":true`) {
		t.Fatalf("set Amen: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/posts", otherToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"amenCount":1`) || strings.Contains(w.Body.String(), `"reacted":true`) {
		t.Fatalf("other member feed reaction: %d %s", w.Code, w.Body.String())
	}
	if w := request("DELETE", "/api/v1/posts/"+post.Data.ID+"/reaction", memberToken, "", "https://api.example"); w.Code != 200 || !strings.Contains(w.Body.String(), `"amenCount":0`) {
		t.Fatalf("remove Amen: %d %s", w.Code, w.Body.String())
	}
	if w := request("PUT", "/api/v1/posts/"+post.Data.ID+"/reaction", memberToken, `{"reaction":"amen"}`, ""); w.Code != 403 {
		t.Fatalf("set Amen without allowed origin: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/posts", otherToken, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Welcome to Sunday service") {
		t.Fatalf("member feed: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct{ path, body string }{{"/api/v1/posts", `{"body":""}`}, {"/api/v1/posts?limit=0", ""}, {"/api/v1/posts?admin=true", ""}} {
		if w := request("POST", "/api/v1/posts", memberToken, tc.body, "https://api.example"); tc.body != "" && w.Code != 422 {
			t.Fatalf("invalid post: %d %s", w.Code, w.Body.String())
		}
		if tc.body == "" {
			if w := request("GET", tc.path, otherToken, "", ""); w.Code != 422 {
				t.Fatalf("invalid feed query %s: %d", tc.path, w.Code)
			}
		}
	}
	if w := request("GET", "/api/v1/posts", "", "", ""); w.Code != 401 {
		t.Fatalf("anonymous feed: %d", w.Code)
	}
	if w := request("POST", "/api/v1/posts", memberToken, `{"body":"No origin"}`, ""); w.Code != 403 {
		t.Fatalf("write without origin: %d", w.Code)
	}
}

func TestPostsContractRequiresMemberAuthentication(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	route := spec.Paths["/api/v1/posts"]
	if route == nil || route["get"] == nil || route["post"] == nil {
		t.Fatal("posts API contract is missing")
	}
	for _, endpoint := range []struct {
		path, method string
	}{{"/api/v1/posts", "get"}, {"/api/v1/posts", "post"}, {"/api/v1/posts/{postId}/reaction", "put"}, {"/api/v1/posts/{postId}/reaction", "delete"}, {"/api/v1/posts/{postId}/comments", "get"}, {"/api/v1/posts/{postId}/comments", "post"}, {"/api/v1/posts/{postId}/comments/{commentId}", "delete"}} {
		security, ok := spec.Paths[endpoint.path][endpoint.method]["security"].([]any)
		if !ok || len(security) != 1 {
			t.Fatalf("%s %s endpoint must require bearer authentication", endpoint.method, endpoint.path)
		}
	}
}

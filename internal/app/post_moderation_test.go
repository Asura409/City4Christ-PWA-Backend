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

func TestPostReportAndModerationRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	const leaderScope = "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, leaderScope); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("r", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email, role, scope string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Report','Tester',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,$3)`, id, role, scope); err != nil {
				t.Fatal(err)
			}
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	authorID, authorToken := account("report-author-route@example.invalid", "", "")
	_, reporterToken := account("report-member-route@example.invalid", "", "")
	_, otherToken := account("report-other-route@example.invalid", "", "")
	_, staffToken := account("report-staff-route@example.invalid", "staff", "")
	_, leaderToken := account("report-leader-route@example.invalid", "ministry_leader", leaderScope)
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
	created := request(http.MethodPost, "/api/v1/posts", authorToken, `{"body":"Reported church update"}`, "https://api.example")
	if created.Code != http.StatusCreated {
		t.Fatalf("create post: %d %s", created.Code, created.Body.String())
	}
	var postResponse struct {
		Data struct {
			ID       string `json:"id"`
			AuthorID string `json:"authorId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &postResponse); err != nil {
		t.Fatal(err)
	}
	if postResponse.Data.AuthorID != authorID {
		t.Fatalf("post author mismatch: %+v", postResponse.Data)
	}
	postID := postResponse.Data.ID
	report := request(http.MethodPost, "/api/v1/content-reports", reporterToken,
		`{"targetType":"post","targetId":"`+postID+`","reason":"harassment","details":"Targets another member."}`, "https://api.example")
	if report.Code != http.StatusCreated || !strings.Contains(report.Body.String(), `"status":"pending"`) {
		t.Fatalf("member report: %d %s", report.Code, report.Body.String())
	}
	if response := request(http.MethodPost, "/api/v1/content-reports", reporterToken,
		`{"targetType":"post","targetId":"`+postID+`","reason":"spam"}`, "https://api.example"); response.Code != http.StatusConflict {
		t.Fatalf("duplicate report: %d %s", response.Code, response.Body.String())
	}
	commentResponse := request(http.MethodPost, "/api/v1/posts/"+postID+"/comments", otherToken, `{"body":"Reported comment"}`, "https://api.example")
	if commentResponse.Code != http.StatusCreated {
		t.Fatalf("create comment: %d %s", commentResponse.Code, commentResponse.Body.String())
	}
	var createdComment struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(commentResponse.Body.Bytes(), &createdComment); err != nil {
		t.Fatal(err)
	}
	commentReport := request(http.MethodPost, "/api/v1/content-reports", reporterToken,
		`{"targetType":"comment","targetId":"`+createdComment.Data.ID+`","reason":"inappropriate"}`, "https://api.example")
	if commentReport.Code != http.StatusCreated {
		t.Fatalf("comment report: %d %s", commentReport.Code, commentReport.Body.String())
	}
	if response := request(http.MethodGet, "/api/v1/moderation/content-reports", reporterToken, "", ""); response.Code != http.StatusForbidden {
		t.Fatalf("member read moderator queue: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodGet, "/api/v1/moderation/content-reports", leaderToken, "", ""); response.Code != http.StatusForbidden {
		t.Fatalf("scoped leader read church-wide queue: %d %s", response.Code, response.Body.String())
	}
	queue := request(http.MethodGet, "/api/v1/moderation/content-reports", staffToken, "", "")
	if queue.Code != http.StatusOK || !strings.Contains(queue.Body.String(), "Reported church update") || !strings.Contains(queue.Body.String(), "Reported comment") {
		t.Fatalf("staff queue: %d %s", queue.Code, queue.Body.String())
	}
	var queueResponse struct {
		Data []struct {
			ID         string `json:"id"`
			TargetType string `json:"targetType"`
		} `json:"data"`
	}
	if err := json.Unmarshal(queue.Body.Bytes(), &queueResponse); err != nil || len(queueResponse.Data) != 2 {
		t.Fatalf("queue response: %+v %v", queueResponse, err)
	}
	var reportID string
	for _, item := range queueResponse.Data {
		if item.TargetType == "post" {
			reportID = item.ID
		}
	}
	if reportID == "" {
		t.Fatal("post report missing from review queue")
	}
	if response := request(http.MethodPost, "/api/v1/moderation/content-reports/"+reportID+"/review", otherToken, `{"decision":"hide","reason":"Not authorized."}`, "https://api.example"); response.Code != http.StatusForbidden {
		t.Fatalf("member reviewed report: %d %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPost, "/api/v1/moderation/content-reports/"+reportID+"/review", staffToken, `{"decision":"hide","reason":"Harassment confirmed."}`, "https://api.example"); response.Code != http.StatusNoContent {
		t.Fatalf("staff hide: %d %s", response.Code, response.Body.String())
	}
	queue = request(http.MethodGet, "/api/v1/moderation/content-reports", staffToken, "", "")
	if queue.Code != http.StatusOK || !strings.Contains(queue.Body.String(), `"data":[]`) {
		t.Fatalf("pending related reports were not resolved: %d %s", queue.Code, queue.Body.String())
	}
	feed := request(http.MethodGet, "/api/v1/posts", reporterToken, "", "")
	if feed.Code != http.StatusOK || strings.Contains(feed.Body.String(), "Reported church update") {
		t.Fatalf("hidden post remains in feed: %d %s", feed.Code, feed.Body.String())
	}
	if response := request(http.MethodPost, "/api/v1/moderation/content-reports/"+reportID+"/review", staffToken, `{"decision":"dismiss","reason":"repeat"}`, "https://api.example"); response.Code != http.StatusConflict {
		t.Fatalf("repeat review was not rejected: %d %s", response.Code, response.Body.String())
	}
}

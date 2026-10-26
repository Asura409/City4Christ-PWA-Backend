package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestSermonProgressAndPrivateNotesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("s", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email string) auth.SessionTokens {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Test','Member',$2) RETURNING id`, email, now).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	member := account("sermon-member@example.invalid")
	other := account("other-sermon-member@example.invalid")
	var sermonID string
	if err := db.QueryRow(`INSERT INTO sermons(title,summary,speaker,series,topic,preached_at,published_at) VALUES('Faith','Summary','Pastor','Series','Faith',$1,$1) RETURNING id`, now).Scan(&sermonID); err != nil {
		t.Fatal(err)
	}
	var draftID string
	if err := db.QueryRow(`INSERT INTO sermons(title,summary,speaker,series,topic,preached_at) VALUES('Draft','Summary','Pastor','Series','Faith',$1) RETURNING id`, now).Scan(&draftID); err != nil {
		t.Fatal(err)
	}
	handler := New(db, Config{VerificationKey: key, AppOrigin: "https://api.example", Now: func() time.Time { return now }})
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", "https://api.example")
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	progressPath := "/api/v1/me/sermons/" + sermonID + "/progress"
	if w := request(http.MethodGet, progressPath, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous progress read status=%d body=%s", w.Code, w.Body.String())
	}
	w := request(http.MethodPut, progressPath, member.AccessToken, `{"positionSeconds":420}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save progress status=%d body=%s", w.Code, w.Body.String())
	}
	var progressResponse struct {
		Data struct {
			PositionSeconds int `json:"positionSeconds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &progressResponse); err != nil || progressResponse.Data.PositionSeconds != 420 {
		t.Fatalf("saved progress response=%s err=%v", w.Body.String(), err)
	}
	w = request(http.MethodGet, progressPath, member.AccessToken, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"positionSeconds":420`) {
		t.Fatalf("read progress status=%d body=%s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{}`, `{"positionSeconds":-1}`, `{"positionSeconds":86401}`} {
		if w = request(http.MethodPut, progressPath, member.AccessToken, body); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid progress %s status=%d body=%s", body, w.Code, w.Body.String())
		}
	}
	if w = request(http.MethodPut, progressPath, member.AccessToken, `{"positionSeconds":1.5}`); w.Code != http.StatusBadRequest {
		t.Fatalf("non-integer progress should be invalid JSON input: status=%d body=%s", w.Code, w.Body.String())
	}
	notePath := "/api/v1/me/sermons/" + sermonID + "/notes"
	w = request(http.MethodPut, notePath, member.AccessToken, `{"body":"Remember this passage."}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Remember this passage.") {
		t.Fatalf("save note status=%d body=%s", w.Code, w.Body.String())
	}
	if w = request(http.MethodGet, notePath, other.AccessToken, ""); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Remember this passage.") {
		t.Fatalf("another member read a private note: status=%d body=%s", w.Code, w.Body.String())
	}
	if w = request(http.MethodGet, notePath, member.AccessToken, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Remember this passage.") {
		t.Fatalf("owner could not read note: status=%d body=%s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"body":"  "}`, `{"body":"` + strings.Repeat("a", 10001) + `"}`} {
		if w = request(http.MethodPut, notePath, member.AccessToken, body); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid note status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if w = request(http.MethodGet, "/api/v1/me/sermons/"+draftID+"/notes", member.AccessToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("draft note access status=%d body=%s", w.Code, w.Body.String())
	}
	if w = request(http.MethodDelete, notePath, member.AccessToken, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete note status=%d body=%s", w.Code, w.Body.String())
	}
	if w = request(http.MethodGet, notePath, member.AccessToken, ""); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Remember this passage.") {
		t.Fatalf("deleted note still visible: status=%d body=%s", w.Code, w.Body.String())
	}
}

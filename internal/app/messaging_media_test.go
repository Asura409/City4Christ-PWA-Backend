package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestDirectMessageAttachmentUsesScannedPrivateMediaAndParticipantAuthorization(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("d", 32))
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	account := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Message','Media',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}
	sender, senderAccess := account("message-media-sender@example.invalid")
	recipient, recipientAccess := account("message-media-recipient@example.invalid")
	_, outsiderAccess := account("message-media-outsider@example.invalid")
	var uploadID string
	if err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES(gen_random_uuid(),$1,'message_attachment','messages/source.png','clean/messages/source.png','image/png',128,'image/png',128,'available',now()+interval '1 hour',now(),now()) RETURNING id::text`, sender).Scan(&uploadID); err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: key, AppOrigin: "https://app.example", MediaStorage: &testMediaStorage{info: media.ObjectInfo{ContentType: "image/png", ContentLength: 128}}})
	request := func(access, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Origin", "https://app.example")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	created := request(senderAccess, http.MethodPost, "/api/v1/conversations", `{"recipientId":"`+recipient+`"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create conversation: %d %s", created.Code, created.Body.String())
	}
	var conversationID string
	if err := db.QueryRow(`SELECT id::text FROM direct_conversations WHERE participant_low IN ($1,$2) AND participant_high IN ($1,$2)`, sender, recipient).Scan(&conversationID); err != nil {
		t.Fatal(err)
	}
	clientMessageID := "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef"
	messagePath := "/api/v1/conversations/" + conversationID + "/messages"
	createdMessage := request(senderAccess, http.MethodPost, messagePath, `{"clientMessageId":"`+clientMessageID+`","body":"See this photo","attachmentIds":["`+uploadID+`"]}`)
	if createdMessage.Code != http.StatusCreated || !strings.Contains(createdMessage.Body.String(), `"attachments":[{"id":"`+uploadID+`"`) || !strings.Contains(createdMessage.Body.String(), "https://r2.example.invalid/clean/messages/source.png") || strings.Contains(createdMessage.Body.String(), "objectKey") {
		t.Fatalf("send attached message: %d %s", createdMessage.Code, createdMessage.Body.String())
	}
	listed := request(recipientAccess, http.MethodGet, messagePath, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"attachments":[{"id":"`+uploadID+`"`) {
		t.Fatalf("recipient history: %d %s", listed.Code, listed.Body.String())
	}
	forbidden := request(outsiderAccess, http.MethodGet, messagePath, "")
	if forbidden.Code != http.StatusNotFound || strings.Contains(forbidden.Body.String(), "signature=test") {
		t.Fatalf("outsider history: %d %s", forbidden.Code, forbidden.Body.String())
	}
	presigned := request(senderAccess, http.MethodPost, "/api/v1/uploads/presign", `{"purpose":"message_attachment","contentType":"video/mp4","size":128}`)
	if presigned.Code != http.StatusCreated || !strings.Contains(presigned.Body.String(), "uploadId") {
		t.Fatalf("message upload reservation: %d %s", presigned.Code, presigned.Body.String())
	}
}

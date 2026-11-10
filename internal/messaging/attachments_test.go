package messaging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/google/uuid"
)

type messageAttachmentSigner struct{}

func (messageAttachmentSigner) PresignDownload(_ context.Context, key string, ttl time.Duration) (media.SignedRequest, error) {
	return media.SignedRequest{URL: "https://media.example.invalid/" + key + "?signature=test", ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

func TestDirectMessagesAttachOnlyOwnedScannedUploadsAndKeepRetryIdempotent(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	sender, recipient, other := seed("message-attachment-sender@example.invalid"), seed("message-attachment-recipient@example.invalid"), seed("message-attachment-other@example.invalid")
	service := NewService(NewRepository(db), nil, messageAttachmentSigner{})
	conversation, _, err := service.CreateConversation(ctx, sender, recipient)
	if err != nil {
		t.Fatal(err)
	}
	clean := messageAttachmentUpload(t, db, sender, "available")
	message, duplicate, err := service.SendMessageWithAttachments(ctx, sender, conversation.ID, "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "See this image", []string{clean})
	if err != nil || duplicate {
		t.Fatalf("send with attachment = (%+v, %v, %v)", message, duplicate, err)
	}
	if len(message.Attachments) != 1 || message.Attachments[0].ID != clean || message.Attachments[0].URL == "" || message.Attachments[0].ObjectKey != "" {
		t.Fatalf("message attachment response = %+v", message.Attachments)
	}
	retry, duplicate, err := service.SendMessageWithAttachments(ctx, sender, conversation.ID, message.ClientMessageID, "See this image", []string{clean})
	if err != nil || !duplicate || retry.ID != message.ID {
		t.Fatalf("same retry = (%+v, %v, %v)", retry, duplicate, err)
	}
	otherUpload := messageAttachmentUpload(t, db, sender, "available")
	if _, _, err := service.SendMessageWithAttachments(ctx, sender, conversation.ID, message.ClientMessageID, "See this image", []string{otherUpload}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("retry with different upload = %v, want idempotency conflict", err)
	}
	if _, err := service.ListMessages(ctx, recipient, conversation.ID, 0, 20); err != nil {
		t.Fatal(err)
	} else if page, _ := service.ListMessages(ctx, recipient, conversation.ID, 0, 20); len(page.Items) != 1 || len(page.Items[0].Attachments) != 1 {
		t.Fatalf("recipient history attachment = %+v", page.Items)
	}
	for label, uploadID := range map[string]string{
		"foreign upload":   messageAttachmentUpload(t, db, other, "available"),
		"uncleared upload": messageAttachmentUpload(t, db, sender, "scan_queued"),
	} {
		if _, _, err := service.SendMessageWithAttachments(ctx, sender, conversation.ID, "300ebbe1-01ca-4c47-a340-03cedf07359e", "Must not attach", []string{uploadID}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s err = %v, want invalid attachment", label, err)
		}
	}
}

func TestGroupMessagesCanAttachCleanFilesForCurrentMembers(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	first, second := seed("group-message-file-first@example.invalid"), seed("group-message-file-second@example.invalid")
	var groupID string
	if err := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('File sharing','', 'church','open',$1) RETURNING id::text`, first).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	for _, accountID := range []string{first, second} {
		if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active')`, groupID, accountID); err != nil {
			t.Fatal(err)
		}
	}
	attachmentID := messageAttachmentUpload(t, db, first, "available")
	service := NewService(NewRepository(db), nil, messageAttachmentSigner{})
	message, duplicate, err := service.SendGroupMessageWithAttachments(ctx, first, groupID, "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "Please see this", []string{attachmentID})
	if err != nil || duplicate || len(message.Attachments) != 1 {
		t.Fatalf("send group attachment = (%+v, %v, %v)", message, duplicate, err)
	}
	page, err := service.ListGroupMessages(ctx, second, groupID, 0, 20)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Attachments) != 1 || page.Items[0].Attachments[0].ID != attachmentID {
		t.Fatalf("member group history = (%+v, %v)", page, err)
	}
}

func TestGroupMessagesReturnSignedSenderProfilePhoto(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	sender, recipient := seed("group-photo-sender@example.invalid"), seed("group-photo-reader@example.invalid")
	var groupID string
	if err := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('Photo group','', 'church','open',$1) RETURNING id::text`, sender).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	for _, accountID := range []string{sender, recipient} {
		if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active')`, groupID, accountID); err != nil {
			t.Fatal(err)
		}
	}
	upload := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at) VALUES($1,$2,'profile_photo',$3,$4,'image/png',8,'image/png',8,'available',now()+interval '1 hour',now(),now())`, upload, sender, "src/"+upload, "profiles/"+upload+"/clean.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET avatar_upload_id=$2 WHERE id=$1`, sender, upload); err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRepository(db), nil, messageAttachmentSigner{})
	if _, _, err := service.SendGroupMessage(ctx, sender, groupID, "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "Hello group"); err != nil {
		t.Fatal(err)
	}
	page, err := service.ListGroupMessages(ctx, recipient, groupID, 0, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].SenderAvatar == nil || !strings.HasPrefix(page.Items[0].SenderAvatar.URL, "https://media.example.invalid/profiles/"+upload+"/clean.png?") {
		t.Fatalf("group sender photo: %+v %v", page, err)
	}
}

func TestSearchMessagesIsCaseInsensitiveAndParticipantScoped(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	sender, recipient, outsider := seed("message-search-sender@example.invalid"), seed("message-search-recipient@example.invalid"), seed("message-search-outsider@example.invalid")
	service := NewService(NewRepository(db), nil, messageAttachmentSigner{})
	conversation, _, err := service.CreateConversation(ctx, sender, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.SendMessage(ctx, sender, conversation.ID, "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "Remember the Morning Prayer"); err != nil {
		t.Fatal(err)
	}
	page, err := service.SearchMessages(ctx, recipient, conversation.ID, " morning ", 0, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].Body != "Remember the Morning Prayer" {
		t.Fatalf("case-insensitive search = (%+v, %v)", page, err)
	}
	if _, err := service.SearchMessages(ctx, outsider, conversation.ID, "prayer", 0, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider search = %v, want not found", err)
	}
	if _, err := service.SearchMessages(ctx, recipient, conversation.ID, "x", 0, 20); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short search = %v, want invalid", err)
	}
}

func TestSearchGroupMessagesRequiresCurrentMembership(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	member, former := seed("group-message-search-member@example.invalid"), seed("group-message-search-former@example.invalid")
	var groupID string
	if err := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('Search group','', 'church','open',$1) RETURNING id::text`, member).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active'),($1,$3,'left')`, groupID, member, former); err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRepository(db), nil, messageAttachmentSigner{})
	if _, _, err := service.SendGroupMessage(ctx, member, groupID, "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "Music rehearsal Friday"); err != nil {
		t.Fatal(err)
	}
	page, err := service.SearchGroupMessages(ctx, member, groupID, "rehearsal", 0, 20)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("member search = (%+v, %v)", page, err)
	}
	if _, err := service.SearchGroupMessages(ctx, former, groupID, "rehearsal", 0, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("former member search = %v, want not found", err)
	}
}

func messageAttachmentUpload(t *testing.T, db *sql.DB, owner, status string) string {
	t.Helper()
	var id string
	key := fmt.Sprintf("messages/%s/%s.png", owner, status+time.Now().UTC().Format("150405.000000000"))
	err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES(gen_random_uuid(),$1,'message_attachment',$2,CASE WHEN $3='available' THEN 'clean/'||$2 ELSE NULL END,'image/png',128,'image/png',128,$3,now()+interval '1 hour',now(),CASE WHEN $3='available' THEN now() ELSE NULL END) RETURNING id::text`, owner, key, status).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

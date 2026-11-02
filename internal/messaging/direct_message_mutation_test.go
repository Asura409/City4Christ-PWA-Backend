package messaging

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSenderCanEditAndDeleteOwnDirectMessageDuringTenMinuteWindow(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	sender, recipient, outsider := seed("direct-mutation-sender@example.invalid"), seed("direct-mutation-recipient@example.invalid"), seed("direct-mutation-outsider@example.invalid")
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(NewRepository(db), func() time.Time { return now }, messageAttachmentSigner{})
	conversation, _, err := service.CreateConversation(ctx, sender, recipient)
	if err != nil {
		t.Fatal(err)
	}
	attachmentID := messageAttachmentUpload(t, db, sender, "available")
	message, _, err := service.SendMessageWithAttachments(ctx, sender, conversation.ID, "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "Original words", []string{attachmentID})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(9 * time.Minute)
	edited, err := service.EditOwnMessage(ctx, sender, conversation.ID, message.ID, "Corrected words")
	if err != nil || edited.Body != "Corrected words" || edited.EditedAt == nil {
		t.Fatalf("edit = (%+v, %v)", edited, err)
	}
	history, err := service.ListMessages(ctx, recipient, conversation.ID, 0, 20)
	if err != nil || len(history.Items) != 1 || history.Items[0].Body != "Corrected words" || history.Items[0].EditedAt == nil {
		t.Fatalf("recipient history after edit = (%+v, %v)", history, err)
	}
	if _, err := service.EditOwnMessage(ctx, outsider, conversation.ID, message.ID, "Unauthorized edit"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider edit = %v, want not found", err)
	}
	if _, err := service.DeleteOwnMessage(ctx, recipient, conversation.ID, message.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recipient deletion = %v, want not found", err)
	}
	if changed, err := service.DeleteOwnMessage(ctx, sender, conversation.ID, message.ID); err != nil || !changed {
		t.Fatalf("sender delete = (%v, %v)", changed, err)
	}
	history, err = service.ListMessages(ctx, recipient, conversation.ID, 0, 20)
	if err != nil || len(history.Items) != 1 || !history.Items[0].Deleted || history.Items[0].Body != "" || len(history.Items[0].Attachments) != 0 {
		t.Fatalf("redacted recipient history = (%+v, %v)", history, err)
	}
	var attachmentLinks int
	if err := db.QueryRow(`SELECT count(*) FROM direct_message_media_attachments WHERE message_id=$1`, message.ID).Scan(&attachmentLinks); err != nil || attachmentLinks != 0 {
		t.Fatalf("redacted message kept %d attachment links, err=%v", attachmentLinks, err)
	}
	var edits, deletions int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE action='edited'),count(*) FILTER (WHERE action='deleted') FROM direct_message_events WHERE message_id=$1`, message.ID).Scan(&edits, &deletions); err != nil || edits != 1 || deletions != 1 {
		t.Fatalf("message audit edits=%d deletions=%d err=%v", edits, deletions, err)
	}
}

func TestSenderCannotEditOrDeleteDirectMessageAfterTenMinutes(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	sender, recipient := seed("direct-mutation-expired-sender@example.invalid"), seed("direct-mutation-expired-recipient@example.invalid")
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(NewRepository(db), func() time.Time { return now })
	conversation, _, err := service.CreateConversation(ctx, sender, recipient)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := service.SendMessage(ctx, sender, conversation.ID, "300ebbe1-01ca-4c47-a340-03cedf07359e", "Keep this record")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := service.EditOwnMessage(ctx, sender, conversation.ID, message.ID, "Too late"); !errors.Is(err, ErrMutationWindowExpired) {
		t.Fatalf("edit at ten-minute boundary = %v, want expired", err)
	}
	if _, err := service.DeleteOwnMessage(ctx, sender, conversation.ID, message.ID); !errors.Is(err, ErrMutationWindowExpired) {
		t.Fatalf("delete at ten-minute boundary = %v, want expired", err)
	}
	history, err := service.ListMessages(ctx, recipient, conversation.ID, 0, 20)
	if err != nil || len(history.Items) != 1 || history.Items[0].Body != "Keep this record" {
		t.Fatalf("expired mutation changed message = (%+v, %v)", history, err)
	}
}

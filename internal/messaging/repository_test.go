package messaging

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/google/uuid"
	"testing"
	"time"
)

func messagingDB(t *testing.T) (*sql.DB, func(string) string) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	seed := func(email string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Direct','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	return db, seed
}

func TestRepositoryCreatesOneConversationPerVerifiedPair(t *testing.T) {
	db, seed := messagingDB(t)
	r := NewRepository(db)
	ctx := context.Background()
	first, second, outsider := seed("dm-first@example.invalid"), seed("dm-second@example.invalid"), seed("dm-outsider@example.invalid")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	one, created, err := r.CreateConversation(ctx, first, second, now)
	if err != nil || !created {
		t.Fatalf("first create = (%+v,%v,%v)", one, created, err)
	}
	two, created, err := r.CreateConversation(ctx, second, first, now.Add(time.Minute))
	if err != nil || created || two.ID != one.ID {
		t.Fatalf("reverse create = (%+v,%v,%v), want same conversation", two, created, err)
	}
	if _, _, err = r.CreateConversation(ctx, first, outsider, now); err != nil {
		t.Fatalf("create other pair: %v", err)
	}
	firstList, err := r.ListConversations(ctx, first)
	if err != nil || len(firstList) != 2 {
		t.Fatalf("first member's conversations = (%+v,%v)", firstList, err)
	}
	outsiderList, err := r.ListConversations(ctx, outsider)
	if err != nil || len(outsiderList) != 1 || outsiderList[0].PartnerID != first {
		t.Fatalf("outsider's conversations = (%+v,%v)", outsiderList, err)
	}
}

func TestConversationReturnsSignedPartnerProfilePhoto(t *testing.T) {
	db, seed := messagingDB(t)
	ctx := context.Background()
	first, second := seed("dm-photo-first@example.invalid"), seed("dm-photo-second@example.invalid")
	var upload string
	upload = uuid.NewString()
	_, err := db.Exec(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at) VALUES($1,$2,'profile_photo',$3,$4,'image/png',8,'image/png',8,'available',now()+interval '1 hour',now(),now())`, upload, second, "src/"+upload, "profiles/"+upload+"/clean.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE accounts SET avatar_upload_id=$2 WHERE id=$1`, second, upload); err != nil {
		t.Fatal(err)
	}
	signer := &testAvatarSigner{}
	service := NewService(NewRepository(db), nil, signer)
	conversation, _, err := service.CreateConversation(ctx, first, second)
	if err != nil || conversation.PartnerAvatar == nil || conversation.PartnerAvatar.URL != "https://cdn.example.invalid/profiles/"+upload+"/clean.png" {
		t.Fatalf("created conversation avatar: %+v %v", conversation.PartnerAvatar, err)
	}
	items, err := service.ListConversations(ctx, first)
	if err != nil || len(items) != 1 || items[0].PartnerAvatar == nil {
		t.Fatalf("listed conversation avatar: %+v %v", items, err)
	}
}

type testAvatarSigner struct{}

func (*testAvatarSigner) PresignDownload(_ context.Context, key string, _ time.Duration) (media.SignedRequest, error) {
	return media.SignedRequest{URL: "https://cdn.example.invalid/" + key}, nil
}

func TestRepositoryPersistsIdempotentMessagesAndMonotonicReadCursor(t *testing.T) {
	db, seed := messagingDB(t)
	r := NewRepository(db)
	ctx := context.Background()
	first, second, outsider := seed("dm-send@example.invalid"), seed("dm-receive@example.invalid"), seed("dm-outside@example.invalid")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	conversation, _, err := r.CreateConversation(ctx, first, second, now)
	if err != nil {
		t.Fatal(err)
	}
	messageID := "7219a6ea-b436-48ca-9d75-b2b8f5dd18ef"
	firstMessage, duplicate, err := r.SendMessage(ctx, conversation.ID, first, messageID, "Please pray with me.", now)
	if err != nil || duplicate {
		t.Fatalf("send = (%+v,%v,%v)", firstMessage, duplicate, err)
	}
	retry, duplicate, err := r.SendMessage(ctx, conversation.ID, first, messageID, "Please pray with me.", now.Add(time.Minute))
	if err != nil || !duplicate || retry.ID != firstMessage.ID || retry.Sequence != firstMessage.Sequence {
		t.Fatalf("idempotent retry = (%+v,%v,%v)", retry, duplicate, err)
	}
	if _, _, err = r.SendMessage(ctx, conversation.ID, first, messageID, "Different content", now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting retry err = %v", err)
	}
	secondMessage, _, err := r.SendMessage(ctx, conversation.ID, second, "300ebbe1-01ca-4c47-a340-03cedf07359e", "I am listening.", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.SendMessage(ctx, conversation.ID, outsider, "b540f6c9-4b1a-4330-aadc-9b0ea54ff1e4", "Intrusion", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider send err = %v", err)
	}
	page, err := r.ListMessages(ctx, conversation.ID, first, 0, 50)
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != firstMessage.ID || page.Items[1].ID != secondMessage.ID {
		t.Fatalf("history = (%+v,%v)", page, err)
	}
	latest, err := r.ListMessages(ctx, conversation.ID, first, 0, 1)
	if err != nil || len(latest.Items) != 1 || latest.Items[0].ID != secondMessage.ID || latest.NextBeforeSequence != secondMessage.Sequence {
		t.Fatalf("latest message page = (%+v,%v)", latest, err)
	}
	older, err := r.ListMessages(ctx, conversation.ID, first, latest.NextBeforeSequence, 1)
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != firstMessage.ID || older.NextBeforeSequence != 0 {
		t.Fatalf("older message page = (%+v,%v)", older, err)
	}
	if _, err = r.ListMessages(ctx, conversation.ID, outsider, 0, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider history err = %v", err)
	}
	later, err := r.MarkRead(ctx, conversation.ID, second, secondMessage.ID, now.Add(3*time.Minute))
	if err != nil || later.LastReadSequence != secondMessage.Sequence {
		t.Fatalf("mark latest = (%+v,%v)", later, err)
	}
	earlier, err := r.MarkRead(ctx, conversation.ID, second, firstMessage.ID, now.Add(4*time.Minute))
	if err != nil || earlier.LastReadSequence != secondMessage.Sequence {
		t.Fatalf("read cursor moved backward: (%+v,%v)", earlier, err)
	}
	firstView, err := r.ListConversations(ctx, first)
	if err != nil || len(firstView) != 1 || firstView[0].PartnerReadSequence != secondMessage.Sequence {
		t.Fatalf("participant receipt = (%+v,%v)", firstView, err)
	}
}

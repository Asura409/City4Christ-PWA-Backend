package media

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMessageDeletionQueuesUnreferencedPrivateObjectForWorkerCleanup(t *testing.T) {
	db := mediaTestDB(t)
	ctx := context.Background()
	owner := mediaTestAccount(t, db)
	peer := mediaTestAccount(t, db)
	conversationID := uuid.NewString()
	low, high := owner, peer
	if high < low {
		low, high = high, low
	}
	if _, err := db.Exec(`INSERT INTO direct_conversations(id,participant_low,participant_high,created_at) VALUES($1,$2,$3,now())`, conversationID, low, high); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{owner, peer} {
		if _, err := db.Exec(`INSERT INTO direct_conversation_members(conversation_id,account_id,joined_at) VALUES($1,$2,now())`, conversationID, id); err != nil {
			t.Fatal(err)
		}
	}
	key := "available/" + uuid.NewString() + "/private.png"
	var uploadID string
	if err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES(gen_random_uuid(),$1,'message_attachment',$2,$3,'image/png',128,'image/png',128,'available',now()+interval '1 hour',now(),now()) RETURNING id::text`, owner, "messages/"+owner+"/source.png", key).Scan(&uploadID); err != nil {
		t.Fatal(err)
	}
	messageIDs := []string{uuid.NewString(), uuid.NewString()}
	for i, id := range messageIDs {
		if _, err := db.Exec(`INSERT INTO direct_messages(id,conversation_id,sender_id,client_message_id,body,created_at) VALUES($1,$2,$3,$4,'Attached photo',now())`, id, conversationID, owner, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO direct_message_media_attachments(message_id,upload_id,position) VALUES($1,$2,0)`, id, uploadID); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	for _, id := range messageIDs {
		detachDirectMessageForTest(t, db, id)
		var status string
		if err := db.QueryRow(`SELECT status FROM media_uploads WHERE id=$1`, uploadID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if id == messageIDs[0] && status != UploadAvailable {
			t.Fatalf("shared upload status after first unlink = %q", status)
		}
	}
	var queued int
	if err := db.QueryRow(`SELECT count(*) FROM media_object_cleanup_jobs WHERE upload_id=$1`, uploadID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("cleanup jobs=%d err=%v; want one", queued, err)
	}
	store := newMemoryScanStorage(key, []byte("private object"), ObjectInfo{})
	processor := NewProcessor(NewRepository(db), store, &recordingScanner{}, ProcessorConfig{LeaseDuration: time.Minute})
	worked, err := processor.ProcessNext(ctx)
	if err != nil || !worked {
		t.Fatalf("cleanup ProcessNext()=(%v,%v), want (true,nil)", worked, err)
	}
	var status string
	var objectKey sql.NullString
	if err := db.QueryRow(`SELECT status,available_object_key FROM media_uploads WHERE id=$1`, uploadID).Scan(&status, &objectKey); err != nil {
		t.Fatal(err)
	}
	if status != UploadDeleted || objectKey.Valid || store.object(key) != nil {
		t.Fatalf("cleaned upload status=%q key=%v objectPresent=%v", status, objectKey, store.object(key) != nil)
	}
	if worked, err := processor.ProcessNext(ctx); err != nil || worked {
		t.Fatalf("empty cleanup ProcessNext()=(%v,%v), want (false,nil)", worked, err)
	}
}

func detachDirectMessageForTest(t *testing.T, db *sql.DB, messageID string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := DetachDirectMessageAttachmentsTx(context.Background(), tx, messageID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

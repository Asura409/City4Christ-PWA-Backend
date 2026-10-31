package messaging

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestMessageNotificationIsPrivateTransactionalAndDeduplicated(t *testing.T) {
	db, seed := messagingDB(t)
	sender, recipient := seed("sender@example.invalid"), seed("recipient@example.invalid")
	r := NewRepository(db)
	ctx := context.Background()
	c, _, e := r.CreateConversation(ctx, sender, recipient, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	client := uuid.NewString()
	for range 2 {
		if _, _, e = r.SendMessage(ctx, c.ID, sender, client, "Confidential message", time.Now()); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e = db.QueryRow(`SELECT count(*) FROM notifications WHERE account_id=$1 AND kind='message.direct' AND body NOT LIKE '%Confidential%'`, recipient).Scan(&count); e != nil || count != 1 {
		t.Fatalf("notice count %d %v", count, e)
	}
	if e = db.QueryRow(`SELECT count(*) FROM notifications WHERE account_id=$1`, sender).Scan(&count); e != nil || count != 0 {
		t.Fatal("sender notified", count, e)
	}
}

package notifications

import (
	"context"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"testing"
)

func TestDeletionPendingAccountCannotReceivePush(t *testing.T) {
	db, r, owner, _ := notificationFixture(t)
	ctx := context.Background()
	s := NewService(r, nil)
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	_, sid, _, err := sessions.AuthenticateSession(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Subscribe(ctx, owner, sid, testSubscription(t)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetPreferences(ctx, owner, Preferences{PushEnabled: true, Groups: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Create(ctx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Synthetic update"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a preexisting queued job: the worker must independently gate state.
	if _, err = db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", owner); err != nil {
		t.Fatal(err)
	}
	sender := &pushStub{status: 201}
	if _, err = NewPushWorker(db, sender, nil).RunOnce(ctx); err != nil || sender.calls != 0 {
		t.Fatal("disabled account received push", sender.calls, err)
	}
}
func TestDeletionPendingAccountIsExcludedFromBroadcastAudience(t *testing.T) {
	db, r, staff, member := notificationFixture(t)
	ctx := context.Background()
	s := NewService(r, nil)
	if _, err := db.Exec("INSERT INTO account_roles(account_id,role,scope)VALUES($1,'staff','')", staff); err != nil {
		t.Fatal(err)
	}
	draft, err := s.CreateBroadcast(ctx, staff, BroadcastInput{Title: "Synthetic update", Body: "Synthetic announcement", Audience: "church"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", member); err != nil {
		t.Fatal(err)
	}
	sent, err := s.SendBroadcast(ctx, staff, draft.ID, draft.Version)
	if err != nil || sent.RecipientCount != 1 {
		t.Fatal("disabled broadcast recipient", sent.RecipientCount, err)
	}
	if count, err := r.UnreadCount(ctx, member); err != nil || count != 0 {
		t.Fatal("disabled inbox recipient", count, err)
	}
}

package pastoral

import (
	"context"
	"testing"
	"time"
)

func TestDeletionPendingPastorIsHiddenAndCannotBeBooked(t *testing.T) {
	db, s, admin, pastor, member, _, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "UTC"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", pastor); err != nil {
		t.Fatal(err)
	}
	page, err := s.Directory(ctx, Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("disabled pastor remained visible", page, err)
	}
	if _, err = s.Request(ctx, member, slot.ID); err == nil {
		t.Fatal("disabled pastor received new appointment")
	}
}
func TestDeletionPendingAccountGetsNoPastoralEmail(t *testing.T) {
	db, _, w, _, pastor, _, _ := emailFixture(t, 48*time.Hour)
	if _, err := db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", pastor); err != nil {
		t.Fatal(err)
	}
	sender := &appointmentRecorder{}
	if _, err := w.DeliverNext(context.Background(), sender); err != nil || len(sender.messages) != 0 {
		t.Fatal("disabled pastor received email", sender.messages, err)
	}
}
func TestDeletionPendingMemberCannotBookPastor(t *testing.T) {
	db, s, admin, pastor, member, _, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "UTC"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", member); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, member, slot.ID); err == nil {
		t.Fatal("disabled member booked appointment")
	}
}

func TestDeletionPendingMemberGetsNoPastoralEmail(t *testing.T) {
	db, s, w, id, pastor, member, _ := emailFixture(t, 48*time.Hour)
	if _, err := s.Decide(context.Background(), pastor, id, Decision{Status: "approved", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", member); err != nil {
		t.Fatal(err)
	}
	sender := &appointmentRecorder{}
	for i := 0; i < 2; i++ {
		if _, err := w.DeliverNext(context.Background(), sender); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.messages) != 0 {
		t.Fatal("disabled member received email", sender.messages)
	}
}

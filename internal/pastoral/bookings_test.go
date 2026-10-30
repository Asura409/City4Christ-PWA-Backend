package pastoral

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBookingsRequireApprovalStayPrivateAndReleaseOnCancellation(t *testing.T) {
	db, s, admin, pastor, member, other, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "Africa/Lagos"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	booked, err := s.Request(ctx, member, slot.ID)
	if err != nil || booked.Status != "pending" {
		t.Fatalf("request %+v %v", booked, err)
	}
	again, err := s.Request(ctx, member, slot.ID)
	if err != nil || again.ID != booked.ID {
		t.Fatal("request retry duplicated", err)
	}
	page, err := s.Availability(ctx, pastor, now, now.Add(24*time.Hour), Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("pending booking did not hold slot", err)
	}
	if _, err = s.Request(ctx, other, slot.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("double booking allowed", err)
	}
	unrelated, err := s.Bookings(ctx, admin, false, Options{})
	if err != nil || len(unrelated.Items) != 0 {
		t.Fatal("admin read others' care bookings", err)
	}
	if _, err = s.Decide(ctx, admin, booked.ID, Decision{Status: "approved", Version: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatal("unassigned administrator approved", err)
	}
	if _, err = s.Cancel(ctx, other, booked.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("other member cancelled", err)
	}
	approved, err := s.Decide(ctx, pastor, booked.ID, Decision{Status: "approved", Version: 1})
	if err != nil || approved.Status != "approved" || approved.Version != 2 {
		t.Fatalf("approve %+v %v", approved, err)
	}
	if _, err = s.Decide(ctx, pastor, booked.ID, Decision{Status: "declined", Version: 1}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale decision accepted", err)
	}
	cancelled, err := s.Cancel(ctx, member, booked.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatal("cancel failed", err)
	}
	if _, err = s.Cancel(ctx, member, booked.ID); err != nil {
		t.Fatal("cancel retry failed", err)
	}
	if _, err = s.Request(ctx, other, slot.ID); err != nil {
		t.Fatal("cancel did not free slot", err)
	}
	var notices int
	if err = db.QueryRow(`SELECT count(*) FROM notifications WHERE kind LIKE 'pastoral.%'`).Scan(&notices); err != nil || notices != 4 {
		t.Fatalf("transactional notification retries: %d %v", notices, err)
	}
}
func TestConcurrentRequestsOnlyReserveOneSlot(t *testing.T) {
	_, s, admin, pastor, member, other, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "UTC"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, actor := range []string{member, other} {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := s.Request(ctx, actor, slot.ID); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
func TestPastorRevocationAndSlotWithdrawalAreEnforced(t *testing.T) {
	db, s, admin, pastor, member, _, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "UTC"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	booking, err := s.Request(ctx, member, slot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WithdrawSlot(ctx, pastor, slot.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("withdraw removed an active appointment", err)
	}
	if _, err = s.Decide(ctx, pastor, booking.ID, Decision{Status: "declined", Version: 1}); err != nil {
		t.Fatal(err)
	}
	// A declined request frees the slot; a new request still needs approval.
	retry, err := s.Request(ctx, member, slot.ID)
	if err != nil {
		t.Fatal("decline did not permit a fresh request", err)
	}
	if _, err = s.Cancel(ctx, member, retry.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.WithdrawSlot(ctx, pastor, slot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, member, slot.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("withdrawn slot booked", err)
	}
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, pastor); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Bookings(ctx, pastor, true, Options{}); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked pastor read bookings", err)
	}
}

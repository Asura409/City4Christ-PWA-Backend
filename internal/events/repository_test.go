package events

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func repositoryFixture(t *testing.T) (*sql.DB, *Repository, string, string, string, time.Time) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	account := func(name string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Event','Member',now()) RETURNING id`, name+"@example.invalid").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	creator, first, second := account("creator"), account("first"), account("second")
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'super_administrator','')`, creator); err != nil {
		t.Fatal(err)
	}
	return db, NewRepository(db), creator, first, second, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
}

func eventInput(now time.Time, capacity *int) CreateInput {
	return CreateInput{Title: "Sunday service", Description: "Worship together", Location: "Main sanctuary", StartsAt: now.Add(24 * time.Hour), TimeZone: "Africa/Lagos", Capacity: capacity}
}

func publish(t *testing.T, repo *Repository, actor, id string, now time.Time, capacity *int) Event {
	t.Helper()
	got, err := repo.Update(context.Background(), actor, id, UpdateInput{Title: "Sunday service", Description: "Worship together", Location: "Main sanctuary", StartsAt: now.Add(24 * time.Hour), TimeZone: "Africa/Lagos", Capacity: capacity, Status: "published", Version: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRepositoryListsOnlyPublishedEventsAndIncludesRSVPCounts(t *testing.T) {
	db, repo, creator, member, _, now := repositoryFixture(t)
	ctx := context.Background()
	capacity := 50
	draft, err := repo.Create(ctx, creator, eventInput(now, &capacity), now)
	if err != nil {
		t.Fatal(err)
	}
	published := publish(t, repo, creator, draft.ID, now, &capacity)
	firstRSVP, err := repo.RSVP(ctx, published.ID, member, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	retryRSVP, err := repo.RSVP(ctx, published.ID, member, 3, now)
	if err != nil || retryRSVP != firstRSVP {
		t.Fatalf("repeat RSVP should be fully idempotent: first=%+v retry=%+v err=%v", firstRSVP, retryRSVP, err)
	}
	var confirmations int
	if err = db.QueryRow(`SELECT count(*) FROM event_rsvp_audit_events WHERE event_id=$1 AND account_id=$2`, published.ID, member).Scan(&confirmations); err != nil || confirmations != 1 {
		t.Fatalf("repeat RSVP added duplicate audit rows: count=%d err=%v", confirmations, err)
	}
	other, err := repo.Create(ctx, creator, CreateInput{Title: "Planning", StartsAt: now.Add(48 * time.Hour), TimeZone: "UTC"}, now)
	if err != nil {
		t.Fatal(err)
	}
	page, err := repo.List(ctx, "", now)
	if err != nil || len(page) != 1 || page[0].ID != published.ID || page[0].AttendeeCount != 3 || page[0].MyAttendeeCount != 0 {
		t.Fatalf("public event list=%+v err=%v; draft %s must stay hidden", page, err, other.ID)
	}
	mine, err := repo.List(ctx, member, now)
	if err != nil || len(mine) != 1 || mine[0].MyAttendeeCount != 3 {
		t.Fatalf("member RSVP summary=%+v err=%v", mine, err)
	}
}

func TestRepositoryRSVPCapacityIsTransactionalAndCancellationFreesSeats(t *testing.T) {
	db, repo, creator, first, second, now := repositoryFixture(t)
	capacity := 3
	draft, err := repo.Create(context.Background(), creator, eventInput(now, &capacity), now)
	if err != nil {
		t.Fatal(err)
	}
	event := publish(t, repo, creator, draft.ID, now, &capacity)
	if event.Capacity == nil || *event.Capacity != capacity {
		t.Fatalf("capacity was not persisted: %+v", event)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, account := range []string{first, second} {
		wg.Add(1)
		go func(account string) {
			defer wg.Done()
			_, err := repo.RSVP(context.Background(), event.ID, account, 2, now)
			results <- err
		}(account)
	}
	wg.Wait()
	close(results)
	succeeded, full := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrFullCapacity):
			full++
		default:
			t.Fatalf("unexpected concurrent RSVP error: %v", err)
		}
	}
	if succeeded != 1 || full != 1 {
		t.Fatalf("capacity race: successful=%d full=%d, want one each", succeeded, full)
	}
	var remaining int
	if err = db.QueryRow(`SELECT COALESCE(sum(attendee_count),0)::int FROM event_rsvps WHERE event_id=$1`, event.ID).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("oversubscribed stored attendee count=%d err=%v", remaining, err)
	}
	var booked string
	if err = db.QueryRow(`SELECT account_id FROM event_rsvps WHERE event_id=$1`, event.ID).Scan(&booked); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RSVP(context.Background(), event.ID, booked, 1, now); err != nil {
		t.Fatal("repeat account update should be capacity-safe:", err)
	}
	if changed, err := repo.CancelRSVP(context.Background(), event.ID, booked, now); err != nil || !changed {
		t.Fatalf("cancel RSVP changed=%v err=%v", changed, err)
	}
	if _, err = repo.RSVP(context.Background(), event.ID, second, 3, now); err != nil {
		t.Fatalf("cancellation did not free seats: %v", err)
	}
}

func TestRepositoryRejectsStaleEventUpdateAndCancellationAfterStart(t *testing.T) {
	_, repo, creator, member, _, now := repositoryFixture(t)
	capacity := 4
	draft, err := repo.Create(context.Background(), creator, eventInput(now, &capacity), now)
	if err != nil {
		t.Fatal(err)
	}
	published := publish(t, repo, creator, draft.ID, now, &capacity)
	if _, err = repo.Update(context.Background(), creator, published.ID, UpdateInput{Title: "Changed", StartsAt: published.StartsAt, TimeZone: published.TimeZone, Status: "published", Version: 1}, now); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version update error=%v", err)
	}
	if _, err = repo.RSVP(context.Background(), published.ID, member, 1, now); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.CancelRSVP(context.Background(), published.ID, member, published.StartsAt); !errors.Is(err, ErrRegistrationClosed) || changed {
		t.Fatalf("post-start cancellation changed=%v err=%v", changed, err)
	}
}

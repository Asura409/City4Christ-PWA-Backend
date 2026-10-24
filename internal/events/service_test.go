package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceRequiresChurchwideEventsManageCapability(t *testing.T) {
	db, repo, creator, member, _, now := repositoryFixture(t)
	ctx := context.Background()
	for role, allowed := range map[string]bool{"assistant_pastor": true, "staff": true} {
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Church','Manager',now()) RETURNING id`, role+"@example.invalid").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,'')`, id, role); err != nil {
			t.Fatal(err)
		}
		event, err := NewService(repo, func() time.Time { return now }).Create(ctx, id, eventInput(now, nil))
		if (err == nil) != allowed || event.ID == "" {
			t.Fatalf("%s create event=%+v err=%v", role, event, err)
		}
	}
	adminEvent, err := NewService(repo, func() time.Time { return now }).Create(ctx, creator, eventInput(now, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewService(repo, func() time.Time { return now }).Create(ctx, member, eventInput(now, nil)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member event creation error=%v", err)
	}
	if _, err = NewService(repo, func() time.Time { return now }).Update(ctx, member, adminEvent.ID, UpdateInput{Title: "Changed", StartsAt: adminEvent.StartsAt, TimeZone: "Africa/Lagos", Status: "published", Version: 1}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member event publication error=%v", err)
	}
}

func TestServiceValidatesTimezoneCapacityAndAttendeeCount(t *testing.T) {
	_, repo, creator, member, _, now := repositoryFixture(t)
	service := NewService(repo, func() time.Time { return now })
	input := eventInput(now, nil)
	input.TimeZone = "Mars/Olympus"
	if _, err := service.Create(context.Background(), creator, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid IANA timezone accepted: %v", err)
	}
	negativeCapacity := -1
	input = eventInput(now, &negativeCapacity)
	if _, err := service.Create(context.Background(), creator, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative capacity accepted: %v", err)
	}
	draft, err := service.Create(context.Background(), creator, eventInput(now, nil))
	if err != nil {
		t.Fatal(err)
	}
	published, err := service.Update(context.Background(), creator, draft.ID, UpdateInput{Title: draft.Title, Description: draft.Description, Location: draft.Location, StartsAt: draft.StartsAt, TimeZone: draft.TimeZone, Status: "published", Version: draft.Version})
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 11} {
		if _, err = service.RSVP(context.Background(), member, published.ID, count); !errors.Is(err, ErrInvalid) {
			t.Fatalf("attendee count %d error=%v", count, err)
		}
	}
}

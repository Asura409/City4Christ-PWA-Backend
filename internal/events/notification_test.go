package events

import (
	"context"
	"testing"
	"time"
)

func TestRSVPMemberReceivesEventChangeNotice(t *testing.T) {
	db, repo, admin, member, _, now := repositoryFixture(t)
	s := NewService(repo, func() time.Time { return now })
	ctx := context.Background()
	ev, e := s.Create(ctx, admin, eventInput(now, nil))
	if e != nil {
		t.Fatal(e)
	}
	ev, e = s.Update(ctx, admin, ev.ID, UpdateInput{Title: ev.Title, StartsAt: ev.StartsAt, TimeZone: ev.TimeZone, Version: ev.Version, Status: "published"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RSVP(ctx, member, ev.ID, 1); e != nil {
		t.Fatal(e)
	}
	_, e = s.Update(ctx, admin, ev.ID, UpdateInput{Title: ev.Title, StartsAt: ev.StartsAt, TimeZone: ev.TimeZone, Version: ev.Version, Status: "cancelled"})
	if e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRow(`SELECT count(*) FROM notifications WHERE account_id=$1 AND kind='event.updated'`, member).Scan(&count); e != nil || count != 1 {
		t.Fatal("missing event notice", count, e)
	}
}

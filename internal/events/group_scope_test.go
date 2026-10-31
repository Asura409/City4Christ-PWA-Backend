package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGroupEventsArePrivateAndManagedByExactLeader(t *testing.T) {
	db, repo, admin, member, other, now := repositoryFixture(t)
	ctx := context.Background()
	s := NewService(repo, func() time.Time { return now })
	var a, b string
	for _, dst := range []*string{&a, &b} {
		if e := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('Group','','church','open',$1) RETURNING id`, admin).Scan(dst); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, *dst); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active')`, a, member); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, other, a); e != nil {
		t.Fatal(e)
	}
	in := eventInput(now, nil)
	in.GroupID = a
	ev, e := s.Create(ctx, other, in)
	if e != nil || ev.GroupID != a {
		t.Fatalf("leader create %+v %v", ev, e)
	}
	in.GroupID = b
	if _, e = s.Create(ctx, other, in); !errors.Is(e, ErrForbidden) {
		t.Fatal("cross group create", e)
	}
	ev, e = s.Update(ctx, other, ev.ID, UpdateInput{Title: ev.Title, StartsAt: ev.StartsAt, TimeZone: ev.TimeZone, Status: "published", Version: ev.Version})
	if e != nil {
		t.Fatal(e)
	}
	items, e := s.List(ctx, "")
	if e != nil || len(items) != 0 {
		t.Fatalf("anonymous leak %+v %v", items, e)
	}
	if _, e = s.Detail(ctx, "", ev.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("detail leak", e)
	}
	if _, e = s.Calendar(ctx, ev.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("calendar leak", e)
	}
	items, e = s.List(ctx, member)
	if e != nil || len(items) != 1 {
		t.Fatalf("member %+v %v", items, e)
	}
	if _, e = s.RSVP(ctx, admin, ev.ID, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("nonmember staff RSVP", e)
	}
	if _, e = s.RSVP(ctx, member, ev.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE group_memberships SET status='left' WHERE account_id=$1`, member); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Detail(ctx, member, ev.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("revocation", e)
	}
	if _, e = s.RSVP(ctx, member, ev.ID, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("revoked RSVP", e)
	}
}

func TestNonmemberGroupManagerCanReviewCoordinators(t *testing.T) {
	db, repo, admin, _, leader, now := repositoryFixture(t)
	ctx := context.Background()
	s := NewService(repo, func() time.Time { return now })
	var g string
	if e := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('Group','','church','open',$1) RETURNING id`, admin).Scan(&g); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, g); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, g); e != nil {
		t.Fatal(e)
	}
	in := eventInput(now, nil)
	in.GroupID = g
	ev, e := s.Create(ctx, leader, in)
	if e != nil {
		t.Fatal(e)
	}
	ev, e = s.Update(ctx, leader, ev.ID, UpdateInput{Title: ev.Title, StartsAt: ev.StartsAt, TimeZone: ev.TimeZone, Version: ev.Version, Status: "published"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Coordinators(ctx, ev.ID, leader); e != nil {
		t.Fatal("manager coordinator review", e)
	}
}

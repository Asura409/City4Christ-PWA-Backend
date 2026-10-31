package notifications

import (
	"context"
	"testing"
)

func TestPreferencesAndMarkAllReadAreAccountScoped(t *testing.T) {
	_, r, owner, other := notificationFixture(t)
	ctx := context.Background()
	s := NewService(r, nil)
	p, e := s.Preferences(ctx, owner)
	if e != nil || p.PushEnabled || !p.Groups || !p.Events || !p.Pastoral || !p.Messages || !p.Announcements {
		t.Fatalf("defaults %+v %v", p, e)
	}
	p.PushEnabled = true
	p.Groups = false
	if _, e = s.SetPreferences(ctx, owner, p); e != nil {
		t.Fatal(e)
	}
	q, e := s.Preferences(ctx, other)
	if e != nil || q.PushEnabled || !q.Groups {
		t.Fatalf("other preferences %+v %v", q, e)
	}
	for _, id := range []string{owner, owner, other} {
		if _, e = r.Create(ctx, CreateInput{AccountID: id, Kind: "group.join_approved", Title: "Update"}); e != nil {
			t.Fatal(e)
		}
	}
	n, e := s.MarkAllRead(ctx, owner)
	if e != nil || n != 2 {
		t.Fatalf("mark all %d %v", n, e)
	}
	n, e = s.MarkAllRead(ctx, owner)
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if count, e := r.UnreadCount(ctx, other); e != nil || count != 1 {
		t.Fatal("other inbox changed", count, e)
	}

}

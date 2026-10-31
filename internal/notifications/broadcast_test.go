package notifications

import (
	"context"
	"errors"
	"testing"
)

func TestBroadcastDraftReviewSendAndScope(t *testing.T) {
	db, r, staff, member := notificationFixture(t)
	ctx := context.Background()
	s := NewService(r, nil)
	if _, e := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'staff','')`, staff); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateBroadcast(ctx, member, BroadcastInput{Title: "No", Body: "Not allowed", Audience: "church"}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	draft, e := s.CreateBroadcast(ctx, staff, BroadcastInput{Title: "Service update", Body: "Synthetic announcement", Audience: "church"})
	if e != nil || draft.Status != "draft" {
		t.Fatalf("draft %+v %v", draft, e)
	}
	if n, e := r.UnreadCount(ctx, member); e != nil || n != 0 {
		t.Fatal("draft delivered", n, e)
	}
	review, e := s.Broadcast(ctx, staff, draft.ID)
	if e != nil || review.RecipientCount != 2 {
		t.Fatal(review, e)
	}
	sent, e := s.SendBroadcast(ctx, staff, draft.ID, draft.Version)
	if e != nil || sent.Status != "sent" || sent.RecipientCount != 2 {
		t.Fatal(sent, e)
	}
	if _, e = s.SendBroadcast(ctx, staff, draft.ID, draft.Version); e != nil {
		t.Fatal("retry", e)
	}
	if n, e := r.UnreadCount(ctx, member); e != nil || n != 1 {
		t.Fatal("duplicate notices", n, e)
	}
	var g string
	if e = db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('A','','church','open',$1) RETURNING id`, staff).Scan(&g); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, g); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, member, g); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateBroadcast(ctx, member, BroadcastInput{Title: "No church-wide", Body: "No", Audience: "church"}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = s.CreateBroadcast(ctx, member, BroadcastInput{Title: "Group", Body: "Group update", Audience: "group", GroupID: g}); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, staff); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Broadcast(ctx, staff, draft.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("revoked broadcaster read", e)
	}
}

func TestGroupBroadcastDisappearsAfterMembershipRevocation(t *testing.T) {
	db, r, staff, member := notificationFixture(t)
	ctx := context.Background()
	s := NewService(r, nil)
	if _, e := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'staff','')`, staff); e != nil {
		t.Fatal(e)
	}
	var g string
	if e := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES('Private','','church','open',$1) RETURNING id`, staff).Scan(&g); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active')`, g, member); e != nil {
		t.Fatal(e)
	}
	b, e := s.CreateBroadcast(ctx, staff, BroadcastInput{Title: "Private update", Body: "Group only", Audience: "group", GroupID: g})
	if e != nil {
		t.Fatal(e)
	}
	b, e = s.SendBroadcast(ctx, staff, b.ID, b.Version)
	if e != nil || b.RecipientCount != 1 {
		t.Fatal(b, e)
	}
	page, e := r.List(ctx, member, Options{})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	id := page.Items[0].ID
	if _, e = db.Exec(`UPDATE group_memberships SET status='left' WHERE account_id=$1`, member); e != nil {
		t.Fatal(e)
	}
	page, e = r.List(ctx, member, Options{})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("revoked inbox leaked", page, e)
	}
	if n, e := r.UnreadCount(ctx, member); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if _, e = s.MarkRead(ctx, member, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("revoked read", e)
	}
}

package notifications

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestStaffBroadcastTargetsOperationalStaffAndHidesAfterRoleRevocation(t *testing.T) {
	db, repo, author, member := notificationFixture(t)
	ctx := context.Background()
	service := NewService(repo, nil)
	addRole := func(account, role string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,'')`, account, role); err != nil {
			t.Fatal(err)
		}
	}
	addRole(author, "staff")
	var admin, assistant, editor string
	for email, destination := range map[string]*string{
		"admin-recipient@example.invalid":  &admin,
		"pastor-recipient@example.invalid": &assistant,
		"editor-recipient@example.invalid": &editor,
	} {
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Staff','Recipient',now()) RETURNING id`, email).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	addRole(admin, "super_administrator")
	addRole(assistant, "assistant_pastor")
	addRole(editor, "content_editor")
	draft, err := service.CreateBroadcast(ctx, author, BroadcastInput{Title: "Staff update", Body: "Team briefing", Audience: "staff"})
	if err != nil {
		t.Fatalf("create staff broadcast: %v", err)
	}
	listed, err := service.Broadcasts(ctx, author, "staff", "")
	if err != nil || len(listed) != 1 || listed[0].ID != draft.ID {
		t.Fatalf("staff broadcast list = %+v, %v", listed, err)
	}
	review, err := service.Broadcast(ctx, author, draft.ID)
	if err != nil || review.RecipientCount != 3 {
		t.Fatalf("staff audience review = %+v, %v; want three operational staff recipients", review, err)
	}
	sent, err := service.SendBroadcast(ctx, author, draft.ID, draft.Version)
	if err != nil || sent.Status != "sent" || sent.Audience != "staff" || sent.RecipientCount != 3 {
		t.Fatalf("staff broadcast send = %+v, %v", sent, err)
	}
	for _, account := range []string{author, admin, assistant} {
		page, listErr := repo.List(ctx, account, Options{})
		if listErr != nil || len(page.Items) != 1 || page.Items[0].Kind != "staff.broadcast" {
			t.Fatalf("staff inbox for %s = %+v, %v", account, page, listErr)
		}
	}
	for _, account := range []string{member, editor} {
		page, listErr := repo.List(ctx, account, Options{})
		if listErr != nil || len(page.Items) != 0 {
			t.Fatalf("nonstaff inbox for %s = %+v, %v", account, page, listErr)
		}
	}
	page, err := repo.List(ctx, author, Options{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("staff author inbox before role revocation = %+v, %v", page, err)
	}
	noticeID := page.Items[0].ID
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1 AND role='staff'`, author); err != nil {
		t.Fatal(err)
	}
	page, err = repo.List(ctx, author, Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("revoked staff role still sees notice: %+v, %v", page, err)
	}
	if count, countErr := repo.UnreadCount(ctx, author); countErr != nil || count != 0 {
		t.Fatalf("revoked staff unread count=(%d,%v)", count, countErr)
	}
	if _, err = repo.MarkRead(ctx, author, noticeID, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked staff marked private notice read: %v", err)
	}
}

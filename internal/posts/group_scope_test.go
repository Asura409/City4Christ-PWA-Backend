package posts

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"testing"
)

func groupPostFixture(t *testing.T) (*sql.DB, *Service, string, string, string, string, string) {
	t.Helper()
	db := testpg.New(t)
	if e := database.Migrate(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	ids := make([]string, 3)
	for i, email := range []string{"author@example.invalid", "peer@example.invalid", "outsider@example.invalid"} {
		if e := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Test','Member',now()) RETURNING id`, email).Scan(&ids[i]); e != nil {
			t.Fatal(e)
		}
	}
	gs := make([]string, 2)
	for i, name := range []string{"A", "B"} {
		if e := db.QueryRow(`INSERT INTO groups(name,description,audience,join_policy,created_by) VALUES($1,'','church','open',$2) RETURNING id`, name, ids[0]).Scan(&gs[i]); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, gs[i]); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range ids[:2] {
		if _, e := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active')`, gs[0], id); e != nil {
			t.Fatal(e)
		}
	}
	return db, NewService(NewRepository(db)), ids[0], ids[1], ids[2], gs[0], gs[1]
}
func TestGroupPostAudienceAndRevocation(t *testing.T) {
	db, s, author, peer, outsider, a, b := groupPostFixture(t)
	ctx := context.Background()
	p, e := s.Create(ctx, author, CreateInput{Body: "Group secret", GroupID: a})
	if e != nil || p.GroupID != a {
		t.Fatalf("create %+v %v", p, e)
	}
	if _, e = s.Create(ctx, outsider, CreateInput{Body: "No", GroupID: a}); !errors.Is(e, ErrNotFound) {
		t.Fatal("nonmember create", e)
	}
	page, e := s.List(ctx, Options{}, outsider)
	if e != nil || len(page.Items) != 0 {
		t.Fatalf("leak %+v %v", page, e)
	}
	page, e = s.List(ctx, Options{GroupID: a}, peer)
	if e != nil || len(page.Items) != 1 {
		t.Fatalf("member %+v %v", page, e)
	}
	if _, e = s.List(ctx, Options{GroupID: b}, peer); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-group list", e)
	}
	comments := NewCommentService(NewCommentRepository(db))
	if _, e = comments.Create(ctx, peer, p.ID, CommentInput{Body: "Private reply"}); e != nil {
		t.Fatal(e)
	}
	if _, e = comments.List(ctx, outsider, p.ID, CommentOptions{}); !errors.Is(e, ErrCommentNotFound) {
		t.Fatal("comments leaked", e)
	}
	if _, e = s.SetAmen(ctx, outsider, p.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("outsider reacted", e)
	}
	if _, e = db.Exec(`UPDATE group_memberships SET status='left' WHERE account_id=$1`, author); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, author, p.ID, UpdateInput{Body: "No longer allowed"}); !errors.Is(e, ErrNotFound) {
		t.Fatal("revoked edit", e)
	}
	if e = s.Delete(ctx, author, p.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("revoked delete", e)
	}
	if _, e = db.Exec(`UPDATE groups SET archived_at=now() WHERE id=$1`, a); e != nil {
		t.Fatal(e)
	}
	if _, e = comments.Create(ctx, peer, p.ID, CommentInput{Body: "Archived"}); !errors.Is(e, ErrCommentNotFound) {
		t.Fatal("archived comment", e)
	}
}
func TestGroupModerationExactScope(t *testing.T) {
	db, s, author, peer, leader, a, b := groupPostFixture(t)
	ctx := context.Background()
	_, e := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, b)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Create(ctx, author, CreateInput{Body: "Reported group text", GroupID: a})
	if e != nil {
		t.Fatal(e)
	}
	m := NewModerationService(NewModerationRepository(db), access.NewService(db))
	r, e := m.Report(ctx, peer, ReportInput{TargetType: "post", TargetID: p.ID, Reason: "spam"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.ListReports(ctx, leader, ReportOptions{GroupID: a}); !errors.Is(e, ErrModerationForbidden) {
		t.Fatal("wrong leader read", e)
	}
	if e = m.Review(ctx, leader, r.ID, ReviewInput{Decision: "hide", Reason: "Reviewed"}); !errors.Is(e, ErrModerationForbidden) {
		t.Fatal("wrong leader review", e)
	}
	if _, e = db.Exec(`UPDATE account_roles SET scope=$2 WHERE account_id=$1`, leader, a); e != nil {
		t.Fatal(e)
	}
	page, e := m.ListReports(ctx, leader, ReportOptions{GroupID: a})
	if e != nil || len(page.Items) != 1 {
		t.Fatalf("correct leader %+v %v", page, e)
	}
	if e = m.Review(ctx, leader, r.ID, ReviewInput{Decision: "hide", Reason: "Reviewed"}); e != nil {
		t.Fatal(e)
	}
}

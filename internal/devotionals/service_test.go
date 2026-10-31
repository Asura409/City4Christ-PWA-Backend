package devotionals

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

func fixture(t *testing.T) (*sql.DB, *Service, string, string) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, name := range []string{"editor", "member"} {
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,$2,'Tester',now()) RETURNING id`, name+"@example.invalid", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'content_editor','')`, ids[0]); err != nil {
		t.Fatal(err)
	}
	return db, NewService(NewRepository(db), func() time.Time { return time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC) }), ids[0], ids[1]
}
func sample() Content {
	return Content{Title: "Hope today", Body: "Reflect and pray.\nA new beginning.", ScriptureReference: "Psalm 28:7", Author: "City4Christ"}
}
func str(s string) *string { return &s }
func TestDraftPublicationAndWithdrawal(t *testing.T) {
	db, s, editor, member := fixture(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, member, sample()); !errors.Is(err, ErrForbidden) {
		t.Fatal("member created draft", err)
	}
	d, err := s.Create(ctx, editor, sample())
	if err != nil || d.Status != "draft" || d.Version != 1 || d.PublishedAt != nil {
		t.Fatal(d, err)
	}
	if _, err = s.GetPublished(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("draft leaked", err)
	}
	if _, err = s.GetEditorial(ctx, member, d.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("private draft read", err)
	}
	page, err := s.ListPublished(ctx, Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("draft listed", page, err)
	}
	edited, err := s.Update(ctx, editor, d.ID, Update{Version: 1, Title: str("Living hope")})
	if err != nil || edited.Version != 2 {
		t.Fatal(edited, err)
	}
	published, err := s.Update(ctx, editor, d.ID, Update{Version: 2, Status: str("published")})
	if err != nil || published.PublishedAt == nil || published.Status != "published" {
		t.Fatal(published, err)
	}
	p, err := s.GetPublished(ctx, d.ID)
	if err != nil || p.Title != "Living hope" {
		t.Fatal(p, err)
	}
	if _, err = s.Update(ctx, editor, d.ID, Update{Version: 2, Status: str("draft")}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale writer won", err)
	}
	if _, err = s.Update(ctx, editor, d.ID, Update{Version: 3, Status: str("draft")}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetPublished(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("withdrawn devotional visible", err)
	}
	var audits int
	if err = db.QueryRow(`SELECT count(*) FROM devotional_audit_events WHERE devotional_id=$1`, d.ID).Scan(&audits); err != nil || audits != 4 {
		t.Fatal("audit missing", audits, err)
	}
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, editor); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, editor, d.ID, Update{Version: 4, Status: str("published")}); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked editor wrote", err)
	}
}
func TestDevotionalValidationAndPagination(t *testing.T) {
	_, s, editor, _ := fixture(t)
	ctx := context.Background()
	for _, content := range []Content{{Title: "", Body: "text"}, {Title: "Title", Body: " "}, {Title: "Unsafe\x00", Body: "text"}} {
		if _, err := s.Create(ctx, editor, content); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad content accepted", err)
		}
	}
	for range 3 {
		d, err := s.Create(ctx, editor, sample())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Update(ctx, editor, d.ID, Update{Version: 1, Status: str("published")}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for range 3 {
		page, err := s.ListPublished(ctx, Options{Limit: 1, Cursor: cursor})
		if err != nil || len(page.Items) != 1 || seen[page.Items[0].ID] {
			t.Fatal("broken pagination", page, err)
		}
		seen[page.Items[0].ID] = true
		cursor = page.NextCursor
	}
	if cursor != "" {
		t.Fatal("extra page")
	}
	if _, err := s.ListPublished(ctx, Options{Cursor: "invalid"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid cursor")
	}
	if _, err := s.ListEditorial(ctx, editor, Options{Limit: 101}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded list")
	}
}
func TestConcurrentDevotionalUpdatesAndAuditRollback(t *testing.T) {
	db, s, editor, _ := fixture(t)
	ctx := context.Background()
	d, err := s.Create(ctx, editor, sample())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Update(ctx, editor, d.ID, Update{Version: 1, Status: str("published")})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	if _, err = db.Exec(`CREATE FUNCTION reject_devotional_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit failure'; END $$; CREATE TRIGGER reject_devotional_audit BEFORE INSERT ON devotional_audit_events FOR EACH ROW EXECUTE FUNCTION reject_devotional_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, editor, d.ID, Update{Version: 2, Status: str("draft")}); err == nil {
		t.Fatal("audit failure ignored")
	}
	kept, err := s.GetEditorial(ctx, editor, d.ID)
	if err != nil || kept.Version != 2 || kept.Status != "published" {
		t.Fatal("partial write", kept, err)
	}
}

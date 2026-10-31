package devotionals

import (
	"context"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"strings"
	"testing"
	"time"
)

func TestDevotionalVisibilityValidationAndMigrationPreservation(t *testing.T) {
	db, s, editor, member := fixture(t)
	ctx := context.Background()
	draft, err := s.Create(ctx, editor, sample())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListEditorial(ctx, member, Options{}); !errors.Is(err, ErrForbidden) {
		t.Fatal("member read editorial list", err)
	}
	for _, u := range []Update{{Version: 0, Status: str("published")}, {Version: 1}, {Version: 1, Status: str("scheduled")}, {Version: 1, Body: str("")}, {Version: 1, Title: str(strings.Repeat("x", 201))}} {
		if _, err = s.Update(ctx, editor, draft.ID, u); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid update", err)
		}
	}
	future, err := s.Create(ctx, editor, sample())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, editor, future.ID, Update{Version: 1, Status: str("published")}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE devotionals SET published_at=$2 WHERE id=$1`, future.ID, s.now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetPublished(ctx, future.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("future publication exposed", err)
	}
	page, err := s.ListPublished(ctx, Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("draft/future publication listed", page, err)
	}
	editorial, err := s.ListEditorial(ctx, editor, Options{Limit: 1})
	if err != nil || len(editorial.Items) != 1 || editorial.NextCursor == "" {
		t.Fatal("editorial pagination", editorial, err)
	}
	next, err := s.ListEditorial(ctx, editor, Options{Limit: 1, Cursor: editorial.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == editorial.Items[0].ID {
		t.Fatal(next, err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetEditorial(ctx, editor, draft.ID); err != nil {
		t.Fatal("migration changed existing draft", err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, editor); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetEditorial(ctx, editor, draft.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("unverified editor read", err)
	}
}

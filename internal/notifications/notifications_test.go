package notifications

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func notificationFixture(t *testing.T) (*sql.DB, *Repository, string, string) {
	t.Helper()
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := func(email string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Inbox','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	return db, NewRepository(db), account("notify-one@example.invalid"), account("notify-two@example.invalid")
}

func TestRepositoryListsPrivateNotificationsUnreadCountAndReadState(t *testing.T) {
	db, repo, owner, other := notificationFixture(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.CreateTx(ctx, tx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Request approved", Body: "You joined Choir.", ResourcePath: "/#/groups/11111111-1111-4111-8111-111111111111", DedupKey: "group:one:approved"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, CreateInput{AccountID: owner, Kind: "group.join_approved", Title: "Request approved", Body: "You joined Choir.", ResourcePath: "/#/groups/11111111-1111-4111-8111-111111111111", DedupKey: "group:one:approved"}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.List(ctx, owner, Options{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != created.ID || page.Items[0].ReadAt != nil {
		t.Fatalf("owner inbox = (%+v,%v)", page, err)
	}
	if count, err := repo.UnreadCount(ctx, owner); err != nil || count != 1 {
		t.Fatalf("unread count=(%d,%v), want 1", count, err)
	}
	if page, err := repo.List(ctx, other, Options{Limit: 20}); err != nil || len(page.Items) != 0 {
		t.Fatalf("other account inbox = (%+v,%v)", page, err)
	}
	if _, err := repo.MarkRead(ctx, other, created.ID, time.Now()); err != ErrNotFound {
		t.Fatalf("other account marked notification read: %v", err)
	}
	changed, err := repo.MarkRead(ctx, owner, created.ID, time.Now())
	if err != nil || !changed {
		t.Fatalf("mark read=(%v,%v)", changed, err)
	}
	changed, err = repo.MarkRead(ctx, owner, created.ID, time.Now().Add(time.Second))
	if err != nil || changed {
		t.Fatalf("repeat mark read=(%v,%v), want idempotent no-op", changed, err)
	}
	if count, err := repo.UnreadCount(ctx, owner); err != nil || count != 0 {
		t.Fatalf("unread count after read=(%d,%v), want 0", count, err)
	}
}

func TestServiceValidatesPaginationAndNotificationInputs(t *testing.T) {
	_, repo, owner, _ := notificationFixture(t)
	service := NewService(repo, nil)
	if _, err := service.List(context.Background(), "not-an-id", Options{Limit: 20}); err != ErrInvalid {
		t.Fatalf("invalid actor accepted: %v", err)
	}
	if _, err := service.List(context.Background(), owner, Options{Limit: 501}); err != ErrInvalid {
		t.Fatalf("excessive page accepted: %v", err)
	}
	for _, input := range []CreateInput{
		{AccountID: owner, Kind: "<script>", Title: "bad", Body: "bad"},
		{AccountID: owner, Kind: "group.join_approved", Title: "", Body: "bad"},
		{AccountID: owner, Kind: "group.join_approved", Title: "ok", Body: "ok", ResourcePath: "https://evil.example"},
	} {
		tx, err := repo.db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.CreateTx(context.Background(), tx, input)
		_ = tx.Rollback()
		if err != ErrInvalid {
			t.Errorf("invalid input %+v error=%v", input, err)
		}
	}
}

func TestRepositoryPaginatesWithStableSequenceCursor(t *testing.T) {
	_, repo, owner, _ := notificationFixture(t)
	ctx := context.Background()
	for index := range 3 {
		if _, err := repo.Create(ctx, CreateInput{AccountID: owner, Kind: "event.published", Title: "Event notice " + strconv.Itoa(index), Body: "A church event was published.", DedupKey: "event:" + strconv.Itoa(index)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.List(ctx, owner, Options{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page=(%+v,%v), want one item and cursor", first, err)
	}
	second, err := repo.List(ctx, owner, Options{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID || second.NextCursor == "" {
		t.Fatalf("second page=(%+v,%v), want next unique item and cursor", second, err)
	}
	third, err := repo.List(ctx, owner, Options{Limit: 1, Cursor: second.NextCursor})
	if err != nil || len(third.Items) != 1 || third.Items[0].ID == second.Items[0].ID || third.NextCursor != "" {
		t.Fatalf("third page=(%+v,%v), want last item without cursor", third, err)
	}
}

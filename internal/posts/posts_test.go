package posts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestRepositoryCreateAndStableFeedPages(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var author string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('author@example.invalid','Ada','Member',now()) RETURNING id`).Scan(&author); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	service := NewService(repo)
	for index, body := range []string{"First post", "Second post", "Third post"} {
		created, err := service.Create(ctx, author, CreateInput{Body: body})
		if err != nil {
			t.Fatal(err)
		}
		if created.FirstName != "Ada" || created.LastName != "Member" {
			t.Fatalf("create response omitted author display name: %+v", created)
		}
		if index == 0 && created.Body != body {
			t.Fatalf("create response omitted body: %+v", created)
		}
	}
	first, err := service.List(ctx, Options{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if first.Items[0].FirstName != "Ada" || first.Items[0].LastName != "Member" || first.Items[0].AuthorID != author {
		t.Fatalf("author identity missing: %+v", first.Items[0])
	}
	second, err := service.List(ctx, Options{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	for _, a := range first.Items {
		if a.ID == second.Items[0].ID {
			t.Fatal("duplicate post across page boundary")
		}
	}
	if _, err := service.Create(ctx, author, CreateInput{Body: strings.Repeat("x", 5001)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized post accepted: %v", err)
	}
	if _, err := service.Create(ctx, author, CreateInput{Body: " \n\t "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty post accepted: %v", err)
	}
	if _, err := service.List(ctx, Options{Cursor: "not a cursor"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad cursor accepted: %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	all, err := service.List(ctx, Options{})
	if err != nil || len(all.Items) != 3 {
		t.Fatalf("migration rerun lost posts: %+v %v", all, err)
	}
}

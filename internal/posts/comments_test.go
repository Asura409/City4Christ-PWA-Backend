package posts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestCommentsArePaginatedMemberVisibleAndOwnerDeletable(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := func(email string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Community','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner, commenter, reader := account("comments-owner@example.invalid"), account("comments-author@example.invalid"), account("comments-reader@example.invalid")
	postService := NewService(NewRepository(db))
	post, err := postService.Create(ctx, owner, CreateInput{Body: "A church update"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewCommentService(NewCommentRepository(db))
	for _, body := range []string{"First encouragement", "Second encouragement", "Third encouragement"} {
		if _, err := service.Create(ctx, commenter, post.ID, CommentInput{Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	feed, err := postService.List(ctx, Options{}, reader)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].CommentCount != 3 {
		t.Fatalf("feed comment count: %+v %v", feed, err)
	}
	first, err := service.List(ctx, reader, post.ID, CommentOptions{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if first.Items[0].AuthorID != commenter || first.Items[0].FirstName != "Community" {
		t.Fatalf("comment author missing: %+v", first.Items[0])
	}
	second, err := service.List(ctx, reader, post.ID, CommentOptions{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	for _, item := range first.Items {
		if item.ID == second.Items[0].ID {
			t.Fatal("duplicate comment across cursor pages")
		}
	}
	if err := service.Delete(ctx, reader, post.ID, first.Items[0].ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("another member deleted comment: %v", err)
	}
	if err := service.Delete(ctx, commenter, post.ID, first.Items[0].ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if err := service.Delete(ctx, commenter, post.ID, first.Items[0].ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("repeat delete: %v", err)
	}
	feed, err = postService.List(ctx, Options{}, reader)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].CommentCount != 2 {
		t.Fatalf("feed comment count after deletion: %+v %v", feed, err)
	}
	if _, err := service.Create(ctx, commenter, post.ID, CommentInput{Body: " \n "}); !errors.Is(err, ErrCommentInvalid) {
		t.Fatalf("blank comment accepted: %v", err)
	}
	if _, err := service.Create(ctx, commenter, post.ID, CommentInput{Body: strings.Repeat("x", 2001)}); !errors.Is(err, ErrCommentInvalid) {
		t.Fatalf("oversized comment accepted: %v", err)
	}
	if _, err := service.List(ctx, reader, post.ID, CommentOptions{Cursor: "not-a-cursor"}); !errors.Is(err, ErrCommentInvalid) {
		t.Fatalf("invalid cursor accepted: %v", err)
	}
	if _, err := service.Create(ctx, commenter, "00000000-0000-4000-8000-000000000001", CommentInput{Body: "Orphan"}); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("comment on missing post accepted: %v", err)
	}
}

package posts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/google/uuid"
)

type postDownloadSigner struct{ calls int }

func (s *postDownloadSigner) PresignDownload(_ context.Context, key string, ttl time.Duration) (media.SignedRequest, error) {
	s.calls++
	return media.SignedRequest{URL: "https://private.r2.example/" + key + "?signature=test", ExpiresAt: time.Now().Add(ttl)}, nil
}

func TestPostAttachmentsRequireOwnedCleanUploadsAndReturnSignedURLs(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var owner, other string
	for email, dest := range map[string]*string{"owner@example.invalid": &owner, "other@example.invalid": &other} {
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Member','Tester',now()) RETURNING id`, email).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	available := insertPostUpload(t, db, owner, "available")
	foreign := insertPostUpload(t, db, other, "available")
	queued := insertPostUpload(t, db, owner, "scan_queued")
	signer := &postDownloadSigner{}
	service := NewService(NewRepository(db), signer)
	created, err := service.Create(ctx, owner, CreateInput{Body: "Post with a photo", AttachmentIDs: []string{available}})
	if err != nil {
		t.Fatalf("Create() with owned clean media: %v", err)
	}
	if len(created.Attachments) != 1 || created.Attachments[0].URL == "" || created.Attachments[0].ContentType != "image/png" || signer.calls != 1 {
		t.Fatalf("created attachment: %+v, signer calls %d", created.Attachments, signer.calls)
	}
	encoded := mustJSON(t, created)
	if strings.Contains(encoded, "objectKey") {
		t.Fatalf("post response leaked storage key: %s", encoded)
	}
	if _, err := service.Create(ctx, owner, CreateInput{Body: "Foreign upload", AttachmentIDs: []string{foreign}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign media accepted: %v", err)
	}
	if _, err := service.Create(ctx, owner, CreateInput{Body: "Still scanning", AttachmentIDs: []string{queued}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unscanned media accepted: %v", err)
	}
	page, err := service.List(ctx, Options{})
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Attachments) != 1 || page.Items[0].Attachments[0].URL == "" {
		t.Fatalf("list with attachment: %+v %v", page, err)
	}
}

func insertPostUpload(t *testing.T, db *sql.DB, owner, status string) string {
	t.Helper()
	var id string
	objectID := uuid.NewString()
	sourceKey := "posts/" + owner + "/" + objectID + ".png"
	availableKey := any(nil)
	uploaded := time.Now()
	var scanned any
	if status == "available" {
		availableKey = "available/" + objectID + "/clean.png"
		scanned = time.Now()
	}
	err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES($1,$2,'post_attachment',$3,$4,'image/png',8,'image/png',8,$5,now()+interval '1 hour',$6,$7) RETURNING id::text`, objectID, owner, sourceKey, availableKey, status, uploaded, scanned).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

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

func TestAmenReactionsAreIdempotentAndScopedPerMember(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := func(email string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Member','Test',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner, first, second := account("reaction-owner@example.invalid"), account("reaction-first@example.invalid"), account("reaction-second@example.invalid")
	service := NewService(NewRepository(db))
	post, err := service.Create(ctx, owner, CreateInput{Body: "A church update"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		state, err := service.SetAmen(ctx, first, post.ID)
		if err != nil || !state.Reacted || state.AmenCount != 1 {
			t.Fatalf("idempotent set %d: %+v %v", i, state, err)
		}
	}
	state, err := service.SetAmen(ctx, second, post.ID)
	if err != nil || state.AmenCount != 2 {
		t.Fatalf("second member reaction: %+v %v", state, err)
	}
	state, err = service.RemoveAmen(ctx, first, post.ID)
	if err != nil || state.Reacted || state.AmenCount != 1 {
		t.Fatalf("remove own reaction: %+v %v", state, err)
	}
	state, err = service.RemoveAmen(ctx, first, post.ID)
	if err != nil || state.AmenCount != 1 {
		t.Fatalf("idempotent remove: %+v %v", state, err)
	}
	page, err := service.List(ctx, Options{}, second)
	if err != nil || len(page.Items) != 1 || page.Items[0].AmenCount != 1 || !page.Items[0].Reacted {
		t.Fatalf("feed reaction summary: %+v %v", page, err)
	}
	if _, err := service.SetAmen(ctx, first, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown post reaction error=%v", err)
	}
}

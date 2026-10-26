package database

import (
	"context"
	"io/fs"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pressly/goose/v3"
)

func TestMediaScanQueueMigrationPreservesExistingRows(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 28); err != nil {
		t.Fatal(err)
	}
	var accountID, postID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at)
		VALUES('media-upgrade@example.invalid','Existing','Member',now()) RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO church_posts(author_id,body) VALUES($1,'existing content') RETURNING id`, accountID).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var body string
	if err := db.QueryRowContext(ctx, `SELECT body FROM church_posts WHERE id=$1`, postID).Scan(&body); err != nil || body != "existing content" {
		t.Fatalf("existing post = %q, err %v", body, err)
	}
	var uploads, jobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_uploads`).Scan(&uploads); err != nil || uploads != 0 {
		t.Fatalf("migration created %d upload rows, err %v; want no seeded data", uploads, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM media_scan_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("migration created %d scan jobs, err %v; want no seeded data", jobs, err)
	}
}

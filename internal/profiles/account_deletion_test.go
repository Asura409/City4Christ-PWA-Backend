package profiles

import (
	"context"
	"testing"
)

func TestDeletionPendingProfileCannotReenterDirectory(t *testing.T) {
	db, r, actor, other := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec("UPDATE accounts SET deletion_requested_at=now(),directory_visible=true WHERE id=$1", other); err != nil {
		t.Fatal(err)
	}
	page, err := NewService(r).Directory(ctx, actor, DirectoryOptions{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("disabled account listed", page, err)
	}
	visible := true
	if _, err := r.Update(ctx, other, Update{DirectoryVisible: &visible}); err == nil {
		t.Fatal("pending profile write accepted")
	}
	if _, err := r.Get(ctx, other); err == nil {
		t.Fatal("disabled profile exposed by own service")
	}
}

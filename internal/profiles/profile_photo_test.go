package profiles

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

type profilePhotoSigner struct{}

func (profilePhotoSigner) PresignDownload(_ context.Context, key string, ttl time.Duration) (media.SignedRequest, error) {
	return media.SignedRequest{URL: "https://r2.example.invalid/" + key + "?signature=private", ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

func TestProfilePhotoLifecycleAndDirectoryPrivacy(t *testing.T) {
	db, repo, viewer, owner := fixture(t)
	ctx := context.Background()
	service := NewService(repo, profilePhotoSigner{})
	if _, err := db.Exec(`UPDATE accounts SET directory_visible=true WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	makeUpload := func(name string) string {
		t.Helper()
		var id string
		key := "profiles/" + owner + "/" + name + ".png"
		err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
			VALUES(gen_random_uuid(),$1,'profile_photo',$2,$3,'image/png',128,'image/png',128,'available',now()+interval '1 hour',now(),now()) RETURNING id::text`, owner, "profiles/"+owner+"/source-"+name+".png", key).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	firstID := makeUpload("first")
	p, err := service.SetAvatar(ctx, owner, firstID)
	if err != nil || p.Avatar == nil || p.Avatar.ID != firstID || !strings.HasPrefix(p.Avatar.URL, "https://r2.example.invalid/profiles/") {
		t.Fatalf("set avatar: %+v %v", p, err)
	}
	listed, err := service.Directory(ctx, viewer, DirectoryOptions{})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].ID != owner || listed.Items[0].Avatar == nil || listed.Items[0].Avatar.ID != firstID {
		t.Fatalf("visible member photo: %+v %v", listed, err)
	}
	if _, err := db.Exec(`UPDATE accounts SET directory_visible=false WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	listed, err = service.Directory(ctx, viewer, DirectoryOptions{})
	if err != nil || len(listed.Items) != 0 {
		t.Fatalf("opted-out member leaked through directory: %+v %v", listed, err)
	}
	own, err := service.Get(ctx, owner)
	if err != nil || own.Avatar == nil || own.Avatar.ID != firstID {
		t.Fatalf("owner lost own private profile photo: %+v %v", own, err)
	}
	secondID := makeUpload("second")
	if _, err := service.SetAvatar(ctx, owner, secondID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM media_uploads WHERE id=$1`, firstID).Scan(&status); err != nil || status != media.UploadDeleting {
		t.Fatalf("replaced upload status = %q, %v; want queued for cleanup", status, err)
	}
	p, err = service.RemoveAvatar(ctx, owner)
	if err != nil || p.Avatar != nil {
		t.Fatalf("remove avatar: %+v %v", p, err)
	}
	if err := db.QueryRow(`SELECT status FROM media_uploads WHERE id=$1`, secondID).Scan(&status); err != nil || status != media.UploadDeleting {
		t.Fatalf("removed upload status = %q, %v; want queued for cleanup", status, err)
	}
	var jobs int
	if err := db.QueryRow(`SELECT count(*) FROM media_object_cleanup_jobs WHERE upload_id IN ($1,$2)`, firstID, secondID).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("cleanup jobs = %d, %v; want two", jobs, err)
	}
	var noPhoto sql.NullString
	if err := db.QueryRow(`SELECT avatar_upload_id::text FROM accounts WHERE id=$1`, owner).Scan(&noPhoto); err != nil || noPhoto.Valid {
		t.Fatalf("avatar association remains after removal: %v %v", noPhoto, err)
	}
}

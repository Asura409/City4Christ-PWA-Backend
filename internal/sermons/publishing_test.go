package sermons

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/google/uuid"
)

type sermonDownloadSigner struct{}

func (sermonDownloadSigner) PresignDownload(_ context.Context, key string, ttl time.Duration) (media.SignedRequest, error) {
	return media.SignedRequest{URL: "https://private.r2.example/" + key + "?signature=test", ExpiresAt: time.Now().Add(ttl)}, nil
}

func TestSermonsAttachOnlyOwnedCleanUploadsAndExposeURLsOnlyWhenPublished(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	staff := editor(t, db, "content_editor")
	other := editor(t, db, "content_editor")
	available := insertSermonUpload(t, db, staff, "available")
	foreign := insertSermonUpload(t, db, other, "available")
	queued := insertSermonUpload(t, db, staff, "scan_queued")
	service := NewService(NewRepository(db), func() time.Time { return now }, sermonDownloadSigner{})
	input := DraftInput{Title: "A message with media", Speaker: "Pastor Joy", PreachedAt: now, AttachmentIDs: []string{available}}
	draft, err := service.CreateDraft(ctx, staff, input)
	if err != nil || len(draft.AttachmentIDs) != 1 || draft.AttachmentIDs[0] != available {
		t.Fatalf("create draft with attachment: %+v %v", draft, err)
	}
	for name, ids := range map[string][]string{"foreign owner": {foreign}, "not scanned": {queued}} {
		bad := input
		bad.AttachmentIDs = ids
		if _, err := service.CreateDraft(ctx, staff, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s attachment accepted: %v", name, err)
		}
	}
	if _, err := service.Get(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("draft attachment was exposed before publication")
	}
	published := "published"
	if _, err := service.Update(ctx, staff, draft.ID, UpdateInput{Version: 1, Status: &published}); err != nil {
		t.Fatal(err)
	}
	sermon, err := service.Get(ctx, draft.ID)
	if err != nil || len(sermon.Attachments) != 1 || sermon.Attachments[0].URL == "" || strings.Contains(mustSermonJSON(t, sermon), "objectKey") {
		t.Fatalf("published attachment response: %+v %v", sermon, err)
	}
}

func insertSermonUpload(t *testing.T, db *sql.DB, owner, status string) string {
	t.Helper()
	var id string
	objectID := uuid.NewString()
	sourceKey := "sermons/" + owner + "/" + objectID + ".png"
	availableKey := any(nil)
	uploaded := time.Now()
	var scanned any
	if status == "available" {
		availableKey = "available/" + objectID + "/clean.png"
		scanned = time.Now()
	}
	err := db.QueryRow(`INSERT INTO media_uploads(id,owner_id,purpose,object_key,available_object_key,declared_content_type,declared_size,actual_content_type,actual_size,status,expires_at,uploaded_at,scanned_at)
		VALUES($1,$2,'sermon_attachment',$3,$4,'image/png',8,'image/png',8,$5,now()+interval '1 hour',$6,$7) RETURNING id::text`, objectID, owner, sourceKey, availableKey, status, uploaded, scanned).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSermonJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func editor(t *testing.T, db *sql.DB, role string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES(gen_random_uuid()::text||'@example.invalid','Editor','Test',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if role != "" {
		if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, id, role); err != nil {
			t.Fatal(err)
		}
	}
	return id
}
func TestPublishingAuthorizationLifecycleAndAudit(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	service := NewService(NewRepository(db), func() time.Time { return now })
	staff := editor(t, db, "content_editor")
	member := editor(t, db, "")
	input := DraftInput{Title: " A message ", Speaker: " Pastor Joy ", Summary: "Summary", PreachedAt: now.Add(-24 * time.Hour), VideoURL: "https://youtu.be/dQw4w9WgXcQ?si=test"}
	if _, err := service.CreateDraft(ctx, member, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member created content: %v", err)
	}
	draft, err := service.CreateDraft(ctx, staff, input)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Version != 1 || draft.Status != "draft" || draft.PublishedAt != nil || draft.Title != "A message" || draft.VideoURL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("draft: %+v", draft)
	}
	if _, err = service.Get(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("draft visible to visitors")
	}
	if _, err = service.GetEditorial(ctx, member, draft.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("member can read a draft")
	}
	published := "published"
	result, err := service.Update(ctx, staff, draft.ID, UpdateInput{Version: 1, Status: &published})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 2 || result.Status != "published" {
		t.Fatalf("publication: %+v", result)
	}
	public, err := service.Get(ctx, draft.ID)
	if err != nil || public.VideoURL != draft.VideoURL {
		t.Fatalf("published detail: %+v %v", public, err)
	}
	if _, err = service.Update(ctx, staff, draft.ID, UpdateInput{Version: 1, Status: &published}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version accepted: %v", err)
	}
	draftStatus := "draft"
	result, err = service.Update(ctx, staff, draft.ID, UpdateInput{Version: 2, Status: &draftStatus})
	if err != nil || result.PublishedAt != nil {
		t.Fatal("could not unpublish")
	}
	if _, err = service.Get(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("unpublished content visible")
	}
	scheduled := "scheduled"
	future := now.Add(time.Hour)
	result, err = service.Update(ctx, staff, draft.ID, UpdateInput{Version: 3, Status: &scheduled, PublishAt: &future})
	if err != nil || result.Status != "scheduled" {
		t.Fatal("could not schedule")
	}
	if _, err = service.Get(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("scheduled content visible early")
	}
	now = future
	if _, err = service.Get(ctx, draft.ID); err != nil {
		t.Fatal("scheduled content not visible at publication boundary")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM sermon_audit_events WHERE sermon_id=$1 AND actor_id=$2`, draft.ID, staff).Scan(&count); err != nil || count != 4 {
		t.Fatalf("missing atomic audit: %d %v", count, err)
	}
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetEditorial(ctx, staff, draft.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("role revocation ineffective")
	}
	if _, err = service.Update(ctx, staff, draft.ID, UpdateInput{Version: 4, Status: &draftStatus}); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked editor changed content")
	}
}
func TestPublishingConcurrentUpdatesAndAuditFailureRollback(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	staff := editor(t, db, "system_administrator")
	service := NewService(NewRepository(db), func() time.Time { return now })
	input := DraftInput{Title: "Draft", Speaker: "Speaker", PreachedAt: now}
	draft, err := service.CreateDraft(ctx, staff, input)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			title := "Edited"
			_, err := service.Update(ctx, staff, draft.ID, UpdateInput{Version: 1, Title: &title})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrVersionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent write lost: %d/%d", success, conflict)
	}
	if _, err = db.Exec(`CREATE FUNCTION reject_sermon_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit failure'; END $$; CREATE TRIGGER reject_sermon_audit BEFORE INSERT ON sermon_audit_events FOR EACH ROW EXECUTE FUNCTION reject_sermon_audit()`); err != nil {
		t.Fatal(err)
	}
	title := "Must roll back"
	if _, err = service.Update(ctx, staff, draft.ID, UpdateInput{Version: 2, Title: &title}); err == nil {
		t.Fatal("update succeeded without audit")
	}
	got, err := service.GetEditorial(ctx, staff, draft.ID)
	if err != nil || got.Version != 2 || got.Title != "Edited" {
		t.Fatalf("audit rollback failed: %+v %v", got, err)
	}
	if _, err = service.CreateDraft(ctx, staff, input); err == nil {
		t.Fatal("create succeeded without audit")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM sermons`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed audited create persisted")
	}
}

func TestPublishingInputAndMediaValidation(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	staff := editor(t, db, "content_editor")
	service := NewService(NewRepository(db), func() time.Time { return now })
	base := DraftInput{Title: "Title", Speaker: "Speaker", PreachedAt: now, ImageURL: "https://images.unsplash.com/photo-1770097286875-0cbf4ca2f8c1?auto=format&fit=crop&w=1200&q=80"}
	for _, url := range []string{"http://youtu.be/dQw4w9WgXcQ", "https://www.youtube.com.evil.example/watch?v=dQw4w9WgXcQ", "https://user:pass@www.youtube.com/watch?v=dQw4w9WgXcQ", "https://127.0.0.1/video", "javascript:alert(1)", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&v=abcdefghijk", "https://youtu.be/bad", "https://youtu.be/dQw4w9WgXcQ#fragment", "https://www.youtube.com:443/watch?v=dQw4w9WgXcQ"} {
		bad := base
		bad.VideoURL = url
		if _, err := service.CreateDraft(ctx, staff, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unsafe media accepted: %s %v", url, err)
		}
	}
	for _, change := range []func(*DraftInput){func(x *DraftInput) { x.Title = " " }, func(x *DraftInput) { x.Speaker = "" }, func(x *DraftInput) { x.Summary = string([]byte{255}) }, func(x *DraftInput) { x.Title = "bad\x00title" }, func(x *DraftInput) { x.PreachedAt = time.Time{} }} {
		bad := base
		change(&bad)
		if _, err := service.CreateDraft(ctx, staff, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid draft accepted")
		}
	}
	draft, err := service.CreateDraft(ctx, staff, base)
	if err != nil {
		t.Fatal(err)
	}
	scheduled, published := "scheduled", "published"
	past := now.Add(-time.Second)
	future := now.Add(time.Hour)
	for _, patch := range []UpdateInput{{Version: 1}, {Version: 1, Status: &scheduled}, {Version: 1, Status: &scheduled, PublishAt: &past}, {Version: 1, Status: &published, PublishAt: &future}, {Version: 1, PublishAt: &future}} {
		if _, err = service.Update(ctx, staff, draft.ID, patch); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid publication accepted: %+v %v", patch, err)
		}
	}
	var events int
	if err = db.QueryRow(`SELECT count(*) FROM sermon_audit_events WHERE sermon_id=$1`, draft.ID).Scan(&events); err != nil || events != 1 {
		t.Fatal("invalid updates changed audit state")
	}
	if _, err = db.Exec(`UPDATE sermons SET video_url='https://evil.example' WHERE id=$1`, draft.ID); err == nil {
		t.Fatal("database accepted unsafe video reference")
	}
	if _, err = db.Exec(`UPDATE sermons SET image_url='https://evil.example/photo-1' WHERE id=$1`, draft.ID); err == nil {
		t.Fatal("database accepted unsafe image reference")
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateDraft(ctx, staff, base); !errors.Is(err, ErrForbidden) {
		t.Fatal("unverified account's role granted publishing")
	}
}

func TestSermonImageURLsAreRestrictedToHTTPSUnsplashCDN(t *testing.T) {
	base := DraftInput{
		Title: "A message", Speaker: "Demo Pastor", PreachedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC),
		ImageURL: "https://images.unsplash.com/photo-1770097286875-0cbf4ca2f8c1?auto=format&fit=crop&w=1200&q=80",
	}
	normalized, err := normalizeDraft(base)
	if err != nil || normalized.ImageURL != base.ImageURL {
		t.Fatalf("valid Unsplash image URL: got %q, %v", normalized.ImageURL, err)
	}
	for _, imageURL := range []string{
		"http://images.unsplash.com/photo-123",
		"https://images.unsplash.com.evil.example/photo-123",
		"https://user@images.unsplash.com/photo-123",
		"https://images.unsplash.com:443/photo-123",
		"https://images.unsplash.com/photo-123?bad=\"quote\"",
		"https://images.unsplash.com/photo-123#fragment",
		"javascript:alert(1)",
		"https://images.unsplash.com/",
	} {
		input := base
		input.ImageURL = imageURL
		_, err := normalizeDraft(input)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["imageUrl"] == "" {
			t.Errorf("image URL %q accepted or missing field error: %v", imageURL, err)
		}
	}
}

func TestSchedulingRespectsPostgresTimestampPrecision(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	staff := editor(t, db, "content_editor")
	service := NewService(NewRepository(db), func() time.Time { return now })
	draft, err := service.CreateDraft(ctx, staff, DraftInput{Title: "Precision", Speaker: "Speaker", PreachedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	status := "scheduled"
	tooClose := now.Add(time.Nanosecond)
	if _, err = service.Update(ctx, staff, draft.ID, UpdateInput{Version: 1, Status: &status, PublishAt: &tooClose}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("sub-microsecond schedule accepted: %v", err)
	}
	invalidUTC := time.Date(9999, 12, 31, 23, 0, 0, 0, time.FixedZone("offset", -12*60*60))
	if _, err = service.CreateDraft(ctx, staff, DraftInput{Title: "Invalid UTC", Speaker: "Speaker", PreachedAt: invalidUTC}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unserializable UTC timestamp accepted: %v", err)
	}
}

func TestPatchPreservesFieldsNotSupplied(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	staff := editor(t, db, "content_editor")
	service := NewService(NewRepository(db), func() time.Time { return now })
	var id string
	summary := "\n  Keep these paragraph breaks.  \n"
	if err := db.QueryRow(`INSERT INTO sermons(title,summary,speaker,series,topic,preached_at) VALUES(' Legacy title ',$1,' Speaker ',' Series ',' Topic ',$2) RETURNING id`, summary, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	published := "published"
	got, err := service.Update(ctx, staff, id, UpdateInput{Version: 1, Status: &published})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != " Legacy title " || got.Summary != summary || got.Speaker != " Speaker " || got.Series != " Series " || got.Topic != " Topic " {
		t.Fatalf("publishing changed omitted metadata: %+v", got)
	}
	title := " New title "
	got, err = service.Update(ctx, staff, id, UpdateInput{Version: 2, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New title" || got.Summary != summary || got.Speaker != " Speaker " {
		t.Fatalf("partial edit changed omitted fields: %+v", got)
	}
}

func TestPendingRoleRevocationCannotBeBypassed(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	staff := editor(t, db, "content_editor")
	service := NewService(NewRepository(db), func() time.Time { return now })
	revocation, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revocation.Rollback()
	if _, err = revocation.ExecContext(ctx, `DELETE FROM account_roles WHERE account_id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	input := DraftInput{Title: "Should not be created", Speaker: "Speaker", PreachedAt: now}
	if _, err = service.CreateDraft(bounded, staff, input); err == nil {
		t.Fatal("write bypassed a locked role revocation")
	}
	if err = revocation.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateDraft(ctx, staff, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked role remained effective: %v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM sermons`).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked editor created content")
	}
}

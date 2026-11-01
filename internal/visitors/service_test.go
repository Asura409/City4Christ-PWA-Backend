package visitors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func visitorFixture(t *testing.T) (*sql.DB, *Service, *time.Time, string, string) {
	t.Helper()
	db := testpg.New(t)
	if e := database.Migrate(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	protection, e := NewProtection("v1", map[string][]byte{"v1": []byte(strings.Repeat("k", 32))})
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, email := range []string{"staff@example.invalid", "existing@example.invalid"} {
		var id string
		if e = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Existing','Account',now()) RETURNING id`, email).Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	if _, e = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'staff','')`, ids[0]); e != nil {
		t.Fatal(e)
	}
	return db, NewService(NewRepository(db), protection, func() time.Time { return now }), &now, ids[0], ids[1]
}
func validSubmission() Submission {
	return Submission{SubmissionID: uuid.NewString(), FullName: "Synthetic Visitor", Email: "existing@example.invalid", Phone: "+441234567890", Consent: true, PolicyVersion: PolicyVersion}
}
func TestVisitorSubmissionIsEncryptedIdempotentAndSeparateFromAccounts(t *testing.T) {
	db, s, _, staff, member := visitorFixture(t)
	ctx := context.Background()
	in := validSubmission()
	if e := s.Submit(ctx, in); e != nil {
		t.Fatal(e)
	}
	if e := s.Submit(ctx, in); e != nil {
		t.Fatal(e)
	}
	var count int
	if e := db.QueryRow(`SELECT count(*) FROM visitor_intakes`).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	var sealed []byte
	if e := db.QueryRow(`SELECT sealed_payload FROM visitor_intakes`).Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{in.FullName, in.Email, in.Phone} {
		if strings.Contains(string(sealed), secret) {
			t.Fatal("plaintext contact stored")
		}
	}
	if e := db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count); e != nil || count != 2 {
		t.Fatal("account created or linked", count, e)
	}
	if e := db.QueryRow(`SELECT count(*) FROM notifications WHERE account_id=$1 AND kind='visitor.received'`, staff).Scan(&count); e != nil || count != 1 {
		t.Fatal("notice duplication", count, e)
	}
	in.FullName = "Changed form"
	if e := s.Submit(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatal("mutated retry", e)
	}
	page, e := s.List(ctx, staff, Options{Limit: 20, Status: "new"})
	if e != nil || len(page.Items) != 1 || page.Items[0].FullName != "Synthetic Visitor" {
		t.Fatal(page, e)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "existing@example.invalid") || strings.Contains(string(raw), "+441234567890") {
		t.Fatal("contacts leaked in list")
	}
	id := page.Items[0].ID
	if _, e = s.Detail(ctx, member, id); !errors.Is(e, ErrForbidden) {
		t.Fatal("member read contact", e)
	}
	detail, e := s.Detail(ctx, staff, id)
	if e != nil || detail.Email != in.Email || detail.ContactVerified {
		t.Fatal(detail, e)
	}
	if _, e = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, staff); e != nil {
		t.Fatal(e)
	}
	if _, e = s.List(ctx, staff, Options{Limit: 20}); !errors.Is(e, ErrForbidden) {
		t.Fatal("revoked staff", e)
	}
}
func TestVisitorConsentStatusRetentionAndWithdrawal(t *testing.T) {
	db, s, now, staff, _ := visitorFixture(t)
	ctx := context.Background()
	in := validSubmission()
	in.Consent = false
	if e := s.Submit(ctx, in); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing consent", e)
	}
	in.Consent = true
	in.PolicyVersion = "old"
	if e := s.Submit(ctx, in); !errors.Is(e, ErrInvalid) {
		t.Fatal("stale policy", e)
	}
	in.PolicyVersion = PolicyVersion
	if e := s.Submit(ctx, in); e != nil {
		t.Fatal(e)
	}
	page, e := s.List(ctx, staff, Options{Limit: 20})
	if e != nil {
		t.Fatal(e)
	}
	id := page.Items[0].ID
	v, e := s.Update(ctx, staff, id, Update{Status: "contacted", Version: 1})
	if e != nil || v.Version != 2 {
		t.Fatal(v, e)
	}
	if _, e = s.Update(ctx, staff, id, Update{Status: "closed", Version: 1}); !errors.Is(e, ErrConflict) {
		t.Fatal("stale update", e)
	}
	*now = now.Add(90 * 24 * time.Hour)
	if _, e = s.Detail(ctx, staff, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("expired contact read", e)
	}
	n, e := PurgeExpired(ctx, db, *now, 100)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	var empty bool
	if e = db.QueryRow(`SELECT sealed_payload IS NULL AND request_digest IS NULL AND status='expired' FROM visitor_intakes WHERE id=$1`, id).Scan(&empty); e != nil || !empty {
		t.Fatal("expiry not scrubbed", e)
	}
	in = validSubmission()
	if e = s.Submit(ctx, in); e != nil {
		t.Fatal(e)
	}
	page, e = s.List(ctx, staff, Options{Limit: 20})
	if e != nil {
		t.Fatal(e)
	}
	id = page.Items[0].ID
	if _, e = s.Update(ctx, staff, id, Update{Status: "withdrawn", Version: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Detail(ctx, staff, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("withdrawn contact read", e)
	}
	if e = s.Submit(ctx, in); e != nil {
		t.Fatal("retry tombstone", e)
	}
	if _, e = s.Detail(ctx, staff, id); !errors.Is(e, ErrNotFound) {
		t.Fatal("resurrected withdrawn visitor", e)
	}
}

func TestVisitorAuditFailureRollsBackAndConcurrentStatusUpdatesConflict(t *testing.T) {
	db, s, _, staff, _ := visitorFixture(t)
	ctx := context.Background()
	in := validSubmission()
	if _, e := db.Exec(`CREATE FUNCTION visitor_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$; CREATE TRIGGER visitor_audit_failure BEFORE INSERT ON visitor_audit_events FOR EACH ROW EXECUTE FUNCTION visitor_audit_failure()`); e != nil {
		t.Fatal(e)
	}
	if e := s.Submit(ctx, in); e == nil {
		t.Fatal("submission without audit")
	}
	var n int
	if e := db.QueryRow(`SELECT count(*) FROM visitor_intakes`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if _, e := db.Exec(`DROP TRIGGER visitor_audit_failure ON visitor_audit_events`); e != nil {
		t.Fatal(e)
	}
	if e := s.Submit(ctx, in); e != nil {
		t.Fatal(e)
	}
	page, e := s.List(ctx, staff, Options{})
	if e != nil {
		t.Fatal(e)
	}
	id := page.Items[0].ID
	results := make(chan error, 2)
	for _, status := range []string{"contacted", "closed"} {
		go func(status string) {
			_, e := s.Update(ctx, staff, id, Update{Status: status, Version: 1})
			results <- e
		}(status)
	}
	success, conflicts := 0, 0
	for range 2 {
		e := <-results
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
}

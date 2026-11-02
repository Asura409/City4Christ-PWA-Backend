package visitors

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestVisitorAssignmentDelegatesOnlyThatConsentedRecord(t *testing.T) {
	db, service, _, staff, volunteer := visitorFixture(t)
	ctx := context.Background()
	in := validSubmission()
	if err := service.Submit(ctx, in); err != nil {
		t.Fatal(err)
	}
	var visitorID string
	if err := db.QueryRow(`SELECT id::text FROM visitor_intakes WHERE submission_id=$1`, in.SubmissionID).Scan(&visitorID); err != nil {
		t.Fatal(err)
	}
	assigned, err := service.Assign(ctx, staff, visitorID, AssignmentInput{AccountID: volunteer, Reason: "Welcome Team follow-up", Version: 1})
	if err != nil || assigned.Version != 2 {
		t.Fatalf("assign %+v %v", assigned, err)
	}
	otherSubmission := validSubmission()
	otherSubmission.FullName = "Unassigned Visitor"
	otherSubmission.Email = "unassigned@example.invalid"
	if err = service.Submit(ctx, otherSubmission); err != nil {
		t.Fatal(err)
	}
	var otherVisitorID string
	if err = db.QueryRow(`SELECT id::text FROM visitor_intakes WHERE submission_id=$1`, otherSubmission.SubmissionID).Scan(&otherVisitorID); err != nil {
		t.Fatal(err)
	}
	page, err := service.List(ctx, volunteer, Options{Status: "new"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != visitorID {
		t.Fatalf("assigned queue %+v %v", page, err)
	}
	if _, err = service.Detail(ctx, volunteer, otherVisitorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("volunteer read another visitor: %v", err)
	}
	contact, err := service.Detail(ctx, volunteer, visitorID)
	if err != nil || contact.Email != in.Email {
		t.Fatalf("assigned detail %+v %v", contact, err)
	}
	updated, err := service.Update(ctx, volunteer, visitorID, Update{Status: "contacted", Version: 2})
	if err != nil || updated.Version != 3 {
		t.Fatalf("assigned status update %+v %v", updated, err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, volunteer); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Detail(ctx, volunteer, visitorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unverified assigned account retained access: %v", err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=now() WHERE id=$1`, volunteer); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, volunteer, visitorID, Update{Status: "withdrawn", Version: 3}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("volunteer withdrew data: %v", err)
	}
	if _, err = service.List(ctx, volunteer, Options{Status: "closed"}); err != nil {
		t.Fatalf("empty assigned status list: %v", err)
	}
	if _, err = service.Assign(ctx, volunteer, visitorID, AssignmentInput{AccountID: staff, Reason: "Escalation", Version: 3}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("volunteer delegated: %v", err)
	}
	if _, err = service.Assign(ctx, staff, visitorID, AssignmentInput{AccountID: "", Reason: "Volunteer unavailable", Version: 3}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if _, err = service.Detail(ctx, volunteer, visitorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked assignment still reads: %v", err)
	}
	page, err = service.List(ctx, staff, Options{Status: "contacted"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != visitorID {
		t.Fatalf("staff lost record %+v %v", page, err)
	}
	var audits int
	if err = db.QueryRow(`SELECT count(*) FROM visitor_audit_events WHERE visitor_id=$1 AND action IN ('assigned','unassigned') AND (assigned_to IS NOT NULL OR previous_assignee_id IS NOT NULL)`, visitorID).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("assignment audit %d %v", audits, err)
	}
}

func TestVisitorAssignmentRequiresCurrentConsentAndActiveAssignee(t *testing.T) {
	db, service, _, staff, volunteer := visitorFixture(t)
	ctx := context.Background()
	in := validSubmission()
	if err := service.Submit(ctx, in); err != nil {
		t.Fatal(err)
	}
	var visitorID string
	if err := db.QueryRow(`SELECT id::text FROM visitor_intakes WHERE submission_id=$1`, in.SubmissionID).Scan(&visitorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE visitor_intakes SET consent_version='visitor-welcome-v1' WHERE id=$1`, visitorID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Assign(ctx, staff, visitorID, AssignmentInput{AccountID: volunteer, Reason: "Legacy form", Version: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("shared old consent: %v", err)
	}
	if _, err := db.Exec(`UPDATE visitor_intakes SET consent_version=$2 WHERE id=$1`, visitorID, PolicyVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, volunteer); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Assign(ctx, staff, visitorID, AssignmentInput{AccountID: volunteer, Reason: "Not verified", Version: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unverified volunteer: %v", err)
	}
	if _, err := service.Assign(ctx, staff, visitorID, AssignmentInput{AccountID: "bad", Reason: "Bad ID", Version: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid assignee: %v", err)
	}
	if _, err := service.Assign(ctx, staff, visitorID, AssignmentInput{AccountID: staff, Reason: "", Version: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing reason: %v", err)
	}
}

func TestVisitorAssignmentAuditFailureRollsBackTheDelegation(t *testing.T) {
	db, service, _, staff, volunteer := visitorFixture(t)
	in := validSubmission()
	if err := service.Submit(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	var visitorID string
	if err := db.QueryRow(`SELECT id::text FROM visitor_intakes WHERE submission_id=$1`, in.SubmissionID).Scan(&visitorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION visitor_assignment_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='assigned' THEN RAISE EXCEPTION 'synthetic assignment audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER visitor_assignment_audit_failure BEFORE INSERT ON visitor_audit_events FOR EACH ROW EXECUTE FUNCTION visitor_assignment_audit_failure()`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Assign(context.Background(), staff, visitorID, AssignmentInput{AccountID: volunteer, Reason: "Welcome Team follow-up", Version: 1}); err == nil {
		t.Fatal("assignment committed without its audit")
	}
	var assigned sql.NullString
	var version int
	if err := db.QueryRow(`SELECT assigned_to::text,version FROM visitor_intakes WHERE id=$1`, visitorID).Scan(&assigned, &version); err != nil || assigned.Valid || version != 1 {
		t.Fatalf("assignment escaped failed audit: %v %d %v", assigned, version, err)
	}
}

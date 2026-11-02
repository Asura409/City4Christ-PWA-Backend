package access

import (
	"context"
	"errors"
	"testing"
)

func TestAdminMemberSearchIsScopedPaginatedAndPrivate(t *testing.T) {
	db, service, actor, _ := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'staff')`, actor); err != nil {
		t.Fatal(err)
	}
	first := account(t, db)
	second := account(t, db)
	third := account(t, db)
	if _, err := db.Exec(`UPDATE accounts SET first_name='Roster',last_name='Member',email='roster.member@example.invalid' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET first_name='Roster',last_name='Second',email='roster.second@example.invalid' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET first_name='Roster',last_name='Third',email='roster.third@example.invalid' WHERE id=$1`, third); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET deletion_requested_at=now() WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	// Restore the first account so one active result remains.
	if _, err := db.Exec(`UPDATE accounts SET deletion_requested_at=NULL WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	page, err := service.SearchMembers(ctx, actor, "roster.member", "", 20)
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("search %+v %v", page, err)
	}
	if page.Data[0].ID != first || page.Data[0].Email != "roster.member@example.invalid" {
		t.Fatalf("result %+v", page.Data[0])
	}
	var searchAudit int
	var fields string
	if err = db.QueryRow(`SELECT count(*),coalesce(max(array_to_string(sensitive_fields,',')),'') FROM member_record_audit_events WHERE actor_id=$1 AND member_id=$2 AND action='search_result'`, actor, first).Scan(&searchAudit, &fields); err != nil || searchAudit != 1 || fields != "email" {
		t.Fatalf("search audit count=%d fields=%q err=%v", searchAudit, fields, err)
	}
	firstPage, err := service.SearchMembers(ctx, actor, "Roster", "", 1)
	if err != nil || len(firstPage.Data) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("first page %+v %v", firstPage, err)
	}
	secondPage, err := service.SearchMembers(ctx, actor, "Roster", firstPage.NextCursor, 1)
	if err != nil || len(secondPage.Data) != 1 || secondPage.Data[0].ID == firstPage.Data[0].ID {
		t.Fatalf("second page %+v %v", secondPage, err)
	}
	for _, query := range []string{"ab", "", "%", "\x00bad"} {
		if _, err := service.SearchMembers(ctx, actor, query, "", 20); !errors.Is(err, ErrInvalid) {
			t.Fatalf("query %q: %v", query, err)
		}
	}
	if _, err := service.SearchMembers(ctx, "00000000-0000-4000-8000-000000000001", "roster", "", 20); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized: %v", err)
	}
	for _, role := range []string{"access_administrator", "content_editor", "finance_officer"} {
		id := account(t, db)
		if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, id, role); err != nil {
			t.Fatal(err)
		}
		if _, err := service.SearchMembers(ctx, id, "roster", "", 20); !errors.Is(err, ErrForbidden) {
			t.Fatalf("role %s unauthorized: %v", role, err)
		}
	}
}

func TestAdminMemberDetailAuditsSensitiveEmailAndFailsClosed(t *testing.T) {
	db, service, actor, target := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role)VALUES($1,'staff')`, actor); err != nil {
		t.Fatal(err)
	}
	member, err := service.MemberDetail(ctx, actor, target)
	if err != nil || member.ID != target || member.Email == "" {
		t.Fatalf("member detail %+v %v", member, err)
	}
	var action string
	var fields string
	if err = db.QueryRow(`SELECT action,array_to_string(sensitive_fields,',') FROM member_record_audit_events WHERE actor_id=$1 AND member_id=$2`, actor, target).Scan(&action, &fields); err != nil || action != "detail_view" || fields != "email" {
		t.Fatalf("member audit action=%q fields=%v err=%v", action, fields, err)
	}
	if _, err = db.Exec(`ALTER TABLE member_record_audit_events ADD CONSTRAINT test_reject_member_detail CHECK(action<>'detail_view') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err = service.MemberDetail(ctx, actor, target); err == nil {
		t.Fatal("private member detail returned without a durable audit")
	}
	for _, role := range []string{"content_editor", "access_administrator", "finance_officer"} {
		unauthorized := account(t, db)
		if _, err = db.Exec(`INSERT INTO account_roles(account_id,role)VALUES($1,$2)`, unauthorized, role); err != nil {
			t.Fatal(err)
		}
		if _, err = service.MemberDetail(ctx, unauthorized, target); !errors.Is(err, ErrForbidden) {
			t.Fatalf("role %s read member detail: %v", role, err)
		}
	}
}

func TestMemberAccessAuditIsSuperAdminOnlyAndPaginated(t *testing.T) {
	db, service, staff, target := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`DELETE FROM account_roles WHERE account_id IN ($1,$2)`, staff, target); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'staff')`, staff); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MemberDetail(ctx, staff, target); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MemberDetail(ctx, staff, target); err != nil {
		t.Fatal(err)
	}
	super := account(t, db)
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'super_administrator')`, super); err != nil {
		t.Fatal(err)
	}
	page, err := service.MemberAccessAudit(ctx, super, target, "", 1)
	if err != nil || len(page.Data) != 1 || page.Data[0].ActorID != staff || page.Data[0].Action != "detail_view" || len(page.Data[0].SensitiveFields) != 1 || page.Data[0].SensitiveFields[0] != "email" || page.NextCursor == "" {
		t.Fatalf("member access audit %+v %v", page, err)
	}
	older, err := service.MemberAccessAudit(ctx, super, target, page.NextCursor, 1)
	if err != nil || len(older.Data) != 1 || older.Data[0].ID == page.Data[0].ID || older.NextCursor != "" {
		t.Fatalf("older member access audit %+v %v", older, err)
	}
	if _, err := service.MemberAccessAudit(ctx, staff, target, "", 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-super-admin audit access: %v", err)
	}
	if _, err := service.MemberAccessAudit(ctx, super, "not-a-uuid", "", 1); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("invalid target: %v", err)
	}
	for _, cursor := range []string{"0", "-1", "not-a-number", "9223372036854775808"} {
		if _, err := service.MemberAccessAudit(ctx, super, target, cursor, 1); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid audit cursor %q: %v", cursor, err)
		}
	}
}

func TestMemberNameCorrectionRequiresReasonVersionAndAudit(t *testing.T) {
	db, service, staff, target := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,'staff')`, staff); err != nil {
		t.Fatal(err)
	}
	first := "Grace"
	updated, err := service.UpdateMemberName(ctx, staff, target, MemberNameUpdate{FirstName: &first, ExpectedVersion: 1, Reason: "Member requested correction"})
	if err != nil || updated.FirstName != first || updated.Version != 2 {
		t.Fatalf("updated name %+v %v", updated, err)
	}
	if _, err = service.UpdateMemberName(ctx, staff, target, MemberNameUpdate{FirstName: &first, ExpectedVersion: 1, Reason: "Stale update"}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale name correction: %v", err)
	}
	var action, fields, reason string
	if err = db.QueryRow(`SELECT action,array_to_string(sensitive_fields,','),reason FROM member_record_audit_events WHERE actor_id=$1 AND member_id=$2`, staff, target).Scan(&action, &fields, &reason); err != nil || action != "name_update" || fields != "firstName" || reason != "Member requested correction" {
		t.Fatalf("audit action=%q fields=%q reason=%q err=%v", action, fields, reason, err)
	}
	for _, input := range []MemberNameUpdate{
		{ExpectedVersion: 2, Reason: "Missing name"},
		{LastName: &first, ExpectedVersion: 0, Reason: "Missing version"},
		{LastName: &first, ExpectedVersion: 2},
	} {
		if _, err = service.UpdateMemberName(ctx, staff, target, input); err == nil {
			t.Fatalf("accepted invalid correction: %+v", input)
		}
	}
	if _, err = service.UpdateMemberName(ctx, "00000000-0000-4000-8000-000000000001", target, MemberNameUpdate{LastName: &first, ExpectedVersion: 2, Reason: "Unauthorized"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized name correction: %v", err)
	}
}

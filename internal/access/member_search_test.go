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

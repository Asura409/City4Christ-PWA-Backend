package pastoral

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"testing"
	"time"
)

func fixture(t *testing.T) (*sql.DB, *Service, string, string, string, string, time.Time) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, name := range []string{"admin", "pastor", "member", "other"} {
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,$2,'Tester',now()) RETURNING id`, name+"@example.invalid", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'super_administrator',''),($2,'assistant_pastor','')`, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	return db, NewService(NewRepository(db), func() time.Time { return now }), ids[0], ids[1], ids[2], ids[3], now
}
func testGuard(context.Context, *sql.Tx) error { return nil }
func TestDirectoryRequiresExplicitAppointmentAndCurrentPastorRole(t *testing.T) {
	db, s, admin, pastor, member, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, member, ProfileInput{AccountID: pastor, TimeZone: "Africa/Lagos"}, testGuard); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member appointed pastor: %v", err)
	}
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: member, TimeZone: "Africa/Lagos"}, testGuard); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-pastor appointed: %v", err)
	}
	p, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, Bio: " Available for prayer. ", TimeZone: "Africa/Lagos"}, testGuard)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != pastor || p.Bio != "Available for prayer." {
		t.Fatal(p)
	}
	page, err := s.Directory(ctx, Options{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("directory: %+v %v", page, err)
	}
	if _, err := db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, pastor); err != nil {
		t.Fatal(err)
	}
	page, err = s.Directory(ctx, Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("revoked pastor remained visible", err)
	}
}
func TestAvailabilityIsPastorOwnedFutureAndNonOverlapping(t *testing.T) {
	_, s, admin, pastor, member, _, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "Africa/Lagos"}, testGuard); err != nil {
		t.Fatal(err)
	}
	in := SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)}
	if _, err := s.CreateSlot(ctx, member, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member scheduled: %v", err)
	}
	slot, err := s.CreateSlot(ctx, pastor, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(75 * time.Minute), EndsAt: now.Add(105 * time.Minute)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("overlap accepted: %v", err)
	}
	if _, err = s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(-time.Hour), EndsAt: now}); !errors.Is(err, ErrInvalid) {
		t.Fatal("past slot accepted")
	}
	page, err := s.Availability(ctx, pastor, now, now.Add(24*time.Hour), Options{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != slot.ID {
		t.Fatalf("availability %+v %v", page, err)
	}
	if _, err = s.Availability(ctx, pastor, now, now.Add(32*24*time.Hour), Options{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded range accepted")
	}
}

func TestProfileRejectsServerLocalTimezone(t *testing.T) {
	_, s, admin, pastor, _, _, _ := fixture(t)
	if _, err := s.Appoint(context.Background(), admin, ProfileInput{AccountID: pastor, TimeZone: "Local"}, testGuard); !errors.Is(err, ErrInvalid) {
		t.Fatalf("server-dependent timezone accepted: %v", err)
	}
}

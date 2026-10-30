package pastoral

import (
	"context"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func TestNotificationFailureRollsBackBookingAndAudit(t *testing.T) {
	db, s, admin, pastor, member, _, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "UTC"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE FUNCTION reject_pastoral_notice() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test notification failure'; END $$; CREATE TRIGGER reject_pastoral_notice BEFORE INSERT ON notifications FOR EACH ROW EXECUTE FUNCTION reject_pastoral_notice();`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, member, slot.ID); err == nil {
		t.Fatal("notification failure was ignored")
	}
	var bookings, audits int
	if err = db.QueryRow(`SELECT count(*) FROM pastor_bookings`).Scan(&bookings); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM pastoral_audit_events WHERE resource_type='booking'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if bookings != 0 || audits != 0 {
		t.Fatal("partial booking survived rollback", bookings, audits)
	}
	if _, err = db.Exec(`DROP TRIGGER reject_pastoral_notice ON notifications`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, member, slot.ID); err != nil {
		t.Fatal("failed attempt kept slot locked", err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM pastor_bookings`).Scan(&bookings); err != nil || bookings != 1 {
		t.Fatal("migration rerun changed booking", err)
	}
}
func TestDatabaseRejectsSecondActiveBookingAndPrivateAdminScope(t *testing.T) {
	db, s, admin, pastor, member, other, now := fixture(t)
	ctx := context.Background()
	for _, id := range []string{pastor, admin} {
		if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: id, TimeZone: "UTC"}, testGuard); err != nil {
			t.Fatal(err)
		}
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(time.Hour), EndsAt: now.Add(90 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, member, slot.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO pastor_bookings(slot_id,pastor_id,member_id,starts_at,ends_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$6)`, slot.ID, pastor, other, slot.StartsAt, slot.EndsAt, now)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != "pastor_bookings_active_slot" {
		t.Fatalf("missing unique reservation constraint: %v", err)
	}
	page, err := s.Bookings(ctx, admin, true, Options{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("Super Admin read another pastor's appointments", err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(ctx, other, slot.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("unverified account requested slot", err)
	}
}

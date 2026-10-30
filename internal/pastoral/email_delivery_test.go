package pastoral

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

type appointmentRecorder struct {
	ids      []string
	messages []AppointmentEmail
	fail     error
}

func (r *appointmentRecorder) SendAppointment(_ context.Context, id string, m AppointmentEmail) error {
	r.ids = append(r.ids, id)
	r.messages = append(r.messages, m)
	return r.fail
}
func emailCount(t *testing.T, db *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pastoral_email_outbox WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func emailFixture(t *testing.T, delay time.Duration) (*sql.DB, *Service, *EmailWorker, string, string, string, *time.Time) {
	t.Helper()
	db, s, admin, pastor, member, _, now := fixture(t)
	ctx := context.Background()
	if _, err := s.Appoint(ctx, admin, ProfileInput{AccountID: pastor, TimeZone: "UTC"}, testGuard); err != nil {
		t.Fatal(err)
	}
	slot, err := s.CreateSlot(ctx, pastor, SlotInput{StartsAt: now.Add(delay), EndsAt: now.Add(delay + 30*time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Request(ctx, member, slot.ID)
	if err != nil {
		t.Fatal(err)
	}
	clock := now
	s.now = func() time.Time { return clock }
	w, err := NewEmailWorker(db, "https://frontend.example.invalid", func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	return db, s, w, b.ID, pastor, member, &clock
}
func TestAppointmentEmailsAreTransactionalAndIdempotent(t *testing.T) {
	db, s, w, id, pastor, member, _ := emailFixture(t, 48*time.Hour)
	ctx := context.Background()
	sender := &appointmentRecorder{}
	if n := emailCount(t, db, "event='requested'"); n != 1 {
		t.Fatal("request not queued", n)
	}
	if _, err := s.Decide(ctx, pastor, id, Decision{Status: "approved", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(ctx, pastor, id, Decision{Status: "approved", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if n := emailCount(t, db, "event='approved'"); n != 1 {
		t.Fatal("approval duplicated", n)
	}
	if n := emailCount(t, db, "event LIKE 'reminder_%'"); n != 4 {
		t.Fatal("missing two reminders for both participants", n)
	}
	// The unsent request is superseded by approval.
	for i := 0; i < 3; i++ {
		if _, err := w.DeliverNext(ctx, sender); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.messages) != 1 || sender.messages[0].Email != "member@example.invalid" {
		t.Fatal("stale/duplicate email", sender.messages)
	}
	if _, err := s.Cancel(ctx, member, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel(ctx, member, id); err != nil {
		t.Fatal(err)
	}
	if n := emailCount(t, db, "event='cancelled'"); n != 2 {
		t.Fatal("both participants need cancellation", n)
	}
	for i := 0; i < 3; i++ {
		if _, err := w.DeliverNext(ctx, sender); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.messages) != 3 {
		t.Fatal("cancellation missing or duplicated", len(sender.messages))
	}
}
func TestAppointmentEmailOutboxFailureRollsBackDecision(t *testing.T) {
	db, s, _, id, pastor, _, _ := emailFixture(t, 48*time.Hour)
	if _, err := db.Exec(`CREATE FUNCTION reject_appointment_email() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$; CREATE TRIGGER reject_appointment_email BEFORE INSERT ON pastoral_email_outbox FOR EACH ROW EXECUTE FUNCTION reject_appointment_email()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(context.Background(), pastor, id, Decision{Status: "approved", Version: 1}); err == nil {
		t.Fatal("ignored outbox failure")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM pastor_bookings WHERE id=$1`, id).Scan(&status); err != nil || status != "pending" {
		t.Fatal("partial decision", status, err)
	}
}
func TestAppointmentEmailRetryKeepsProviderKeyAndStopsAtLimit(t *testing.T) {
	db, _, w, _, _, _, clock := emailFixture(t, 48*time.Hour)
	ctx := context.Background()
	sender := &appointmentRecorder{fail: &auth.EmailDeliveryError{Code: "provider_unavailable", Retryable: true}}
	for i := 0; i < 5; i++ {
		processed, err := w.DeliverNext(ctx, sender)
		if !processed || err == nil {
			t.Fatal("failed send not tracked", processed, err)
		}
		if i == 0 {
			processed, err = w.DeliverNext(ctx, sender)
			if processed || err != nil {
				t.Fatal("backoff ignored", processed, err)
			}
		}
		*clock = clock.Add(2 * time.Minute)
	}
	if _, err := w.DeliverNext(ctx, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.ids) != 5 || emailCount(t, db, "failed_at IS NOT NULL") != 1 {
		t.Fatal("retry limit ignored")
	}
	for _, id := range sender.ids {
		if id != sender.ids[0] {
			t.Fatal("provider key changed")
		}
	}
}
func TestAppointmentEmailRejectsUnsafeAppURL(t *testing.T) {
	for _, u := range []string{"", "http://frontend.example", "https://u:p@frontend.example", "https://frontend.example?token=x", "https://frontend.example/#/login"} {
		if _, err := NewEmailWorker(nil, u, nil); err == nil {
			t.Fatal("unsafe URL", u)
		}
	}
	if _, err := NewEmailWorker(nil, "https://frontend.example/City4Christ-PWA/", nil); err != nil {
		t.Fatal(err)
	}
}

package pastoral

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func drainAppointmentEmails(t *testing.T, w *EmailWorker, s *appointmentRecorder) {
	t.Helper()
	for i := 0; i < 30; i++ {
		did, err := w.DeliverNext(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			return
		}
	}
	t.Fatal("queue did not drain")
}
func TestAppointmentRemindersRespectScheduleCancellationAndExpiry(t *testing.T) {
	db, s, w, id, pastor, member, clock := emailFixture(t, 48*time.Hour)
	ctx := context.Background()
	rec := &appointmentRecorder{}
	if _, err := s.Decide(ctx, pastor, id, Decision{Status: "approved", Version: 1}); err != nil {
		t.Fatal(err)
	}
	drainAppointmentEmails(t, w, rec)
	rec.messages = nil
	*clock = clock.Add(24*time.Hour - time.Second)
	drainAppointmentEmails(t, w, rec)
	if len(rec.messages) != 0 {
		t.Fatal("early reminder")
	}
	*clock = clock.Add(time.Second)
	drainAppointmentEmails(t, w, rec)
	if len(rec.messages) != 2 || !rec.messages[0].Reminder || !rec.messages[1].Reminder {
		t.Fatal("24h reminder missing", rec.messages)
	}
	drainAppointmentEmails(t, w, rec)
	if len(rec.messages) != 2 {
		t.Fatal("reminder repeated")
	}
	if _, err := s.Cancel(ctx, member, id); err != nil {
		t.Fatal(err)
	}
	drainAppointmentEmails(t, w, rec)
	rec.messages = nil
	*clock = clock.Add(23 * time.Hour)
	drainAppointmentEmails(t, w, rec)
	if len(rec.messages) != 0 {
		t.Fatal("cancelled appointment reminded")
	}
	if emailCount(t, db, "event='reminder_1h' AND discarded_at IS NOT NULL") != 2 {
		t.Fatal("cancelled reminder not discarded")
	}
	// A worker outage must not cause delivery once an appointment has begun.
	db2, s2, w2, id2, pastor2, _, clock2 := emailFixture(t, 48*time.Hour)
	if _, err := s2.Decide(ctx, pastor2, id2, Decision{Status: "approved", Version: 1}); err != nil {
		t.Fatal(err)
	}
	*clock2 = clock2.Add(48 * time.Hour)
	rec.messages = nil
	drainAppointmentEmails(t, w2, rec)
	if len(rec.messages) != 0 || emailCount(t, db2, "discarded_at IS NOT NULL") != 6 {
		t.Fatal("expired emails sent")
	}
}
func TestAppointmentReminderOneHourAndShortNoticeApproval(t *testing.T) {
	for _, delay := range []time.Duration{2 * time.Hour, 30 * time.Minute} {
		t.Run(delay.String(), func(t *testing.T) {
			db, s, w, id, pastor, _, clock := emailFixture(t, delay)
			ctx := context.Background()
			rec := &appointmentRecorder{}
			if _, err := s.Decide(ctx, pastor, id, Decision{Status: "approved", Version: 1}); err != nil {
				t.Fatal(err)
			}
			want := 2
			if delay < time.Hour {
				want = 0
			}
			if emailCount(t, db, "event LIKE 'reminder_%'") != want {
				t.Fatal("late reminders queued")
			}
			drainAppointmentEmails(t, w, rec)
			rec.messages = nil
			if delay > time.Hour {
				*clock = clock.Add(time.Hour)
				drainAppointmentEmails(t, w, rec)
				if len(rec.messages) != 2 || !rec.messages[0].Reminder {
					t.Fatal("1h reminder missing")
				}
			}
		})
	}
}
func TestAppointmentDeliveryRechecksVerificationAndPastoralRole(t *testing.T) {
	for _, change := range []string{"unverified", "revoked", "unlisted", "declined"} {
		t.Run(change, func(t *testing.T) {
			db, s, w, id, pastor, member, _ := emailFixture(t, 48*time.Hour)
			ctx := context.Background()
			rec := &appointmentRecorder{}
			if change == "declined" {
				if _, err := s.Decide(ctx, pastor, id, Decision{Status: "declined", Version: 1}); err != nil {
					t.Fatal(err)
				}
				drainAppointmentEmails(t, w, rec)
				if len(rec.messages) != 1 || rec.messages[0].Email != "member@example.invalid" {
					t.Fatal("decline notification", rec.messages)
				}
				return
			}
			if _, err := s.Decide(ctx, pastor, id, Decision{Status: "approved", Version: 1}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "unverified":
				_, err = db.Exec(`UPDATE accounts SET email_verified_at=NULL WHERE id=$1`, member)
			case "revoked":
				_, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, pastor)
			case "unlisted":
				_, err = db.Exec(`UPDATE pastor_profiles SET listed=false WHERE account_id=$1`, pastor)
			}
			if err != nil {
				t.Fatal(err)
			}
			drainAppointmentEmails(t, w, rec)
			want := 0
			if change == "unlisted" {
				want = 1
			}
			if len(rec.messages) != want {
				t.Fatal("recipient authority ignored", rec.messages)
			}
		})
	}
}

type blockingAppointmentSender struct {
	started chan string
	release chan struct{}
}

func (s blockingAppointmentSender) SendAppointment(ctx context.Context, id string, _ AppointmentEmail) error {
	s.started <- id
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
		return nil
	}
}
func TestAppointmentConcurrentWorkersAndCancellationSerialize(t *testing.T) {
	_, s, w, id, _, member, _ := emailFixture(t, 48*time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	provider := blockingAppointmentSender{started: make(chan string, 1), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := w.DeliverNext(ctx, provider); done <- err }()
	select {
	case <-provider.started:
	case <-ctx.Done():
		t.Fatal("send not started")
	}
	rec := &appointmentRecorder{}
	if did, err := w.DeliverNext(ctx, rec); did || err != nil || len(rec.messages) != 0 {
		t.Fatal("concurrent worker sent same item", did, err)
	}
	cancelling := make(chan error, 1)
	go func() { _, err := s.Cancel(ctx, member, id); cancelling <- err }()
	select {
	case err := <-cancelling:
		t.Fatal("mutation bypassed in-flight send", err)
	case <-time.After(40 * time.Millisecond):
	}
	close(provider.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-cancelling; err != nil {
		t.Fatal(err)
	}
	drainAppointmentEmails(t, w, rec)
	if len(rec.messages) != 2 {
		t.Fatal("cancellation emails missing")
	}
}
func TestAppointmentPermanentErrorsAndRateLimitRetryAreSafe(t *testing.T) {
	for _, kind := range []string{"rejected", "rate-limited", "unclassified"} {
		t.Run(kind, func(t *testing.T) {
			db, _, w, _, _, _, clock := emailFixture(t, 48*time.Hour)
			ctx := context.Background()
			failure := error(&auth.EmailDeliveryError{Code: "provider_rejected"})
			if kind == "rate-limited" {
				failure = &auth.EmailDeliveryError{Code: "provider_rate_limited", Retryable: true, RetryAfter: time.Minute}
			}
			if kind == "unclassified" {
				failure = errors.New("private provider response member@example.invalid secret")
			}
			rec := &appointmentRecorder{fail: failure}
			_, err := w.DeliverNext(ctx, rec)
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "@") {
				t.Fatal("unsafe error", err)
			}
			if kind == "rejected" {
				if emailCount(t, db, "failed_at IS NOT NULL") != 1 {
					t.Fatal("permanent failure retried")
				}
				return
			}
			if kind == "rate-limited" {
				*clock = clock.Add(59 * time.Second)
				if did, err := w.DeliverNext(ctx, rec); did || err != nil {
					t.Fatal("Retry-After ignored")
				}
				*clock = clock.Add(time.Second)
			} else {
				*clock = clock.Add(5 * time.Second)
			}
			rec.fail = nil
			if did, err := w.DeliverNext(ctx, rec); !did || err != nil {
				t.Fatal("retry missing", err)
			}
		})
	}
}

type acceptedThenCancelled struct {
	cancel context.CancelFunc
	ids    []string
}

func (s *acceptedThenCancelled) SendAppointment(_ context.Context, id string, _ AppointmentEmail) error {
	s.ids = append(s.ids, id)
	s.cancel()
	return nil
}
func TestAppointmentAcceptedSendWithLostCommitKeepsDeduplicationKey(t *testing.T) {
	_, _, w, _, _, _, _ := emailFixture(t, 48*time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	sender := &acceptedThenCancelled{cancel: cancel}
	if _, err := w.DeliverNext(ctx, sender); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown incorrectly recorded success", err)
	}
	rec := &appointmentRecorder{}
	if did, err := w.DeliverNext(context.Background(), rec); !did || err != nil {
		t.Fatal("retry failed", err)
	}
	if len(rec.ids) != 1 || rec.ids[0] != sender.ids[0] {
		t.Fatal("accepted send retried under a different provider key")
	}
}

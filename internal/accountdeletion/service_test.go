package accountdeletion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/jackc/pgx/v5/pgconn"
)

func fixture(t *testing.T) (*sql.DB, *Service, string) {
	t.Helper()
	db := testpg.New(t)
	if e := database.Migrate(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	return db, NewService(db, func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }), account(t, db)
}
func account(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if e := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,directory_visible)VALUES(gen_random_uuid()::text||'@example.invalid','Synthetic','Member',now(),true)RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func guard(context.Context, *sql.Tx) error { return nil }
func admin(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := account(t, db)
	_, e := db.Exec(`WITH s AS(INSERT INTO auth_sessions(id,account_id,expires_at)VALUES(gen_random_uuid(),$1,now()+interval '1 day')RETURNING id)INSERT INTO mfa_factors(account_id,id,sealed_secret,enrollment_session_id,pending_expires_at,confirmed_at)SELECT $1,gen_random_uuid(),'secret'::bytea,id,now(),now()FROM s`, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO account_roles(account_id,role)VALUES($1,'super_administrator')`, id); e != nil {
		t.Fatal(e)
	}
	return id
}

func TestRequestDisablesWithoutDeletingAndIsRepeatSafe(t *testing.T) {
	db, s, id := fixture(t)
	ctx := context.Background()
	reviewer := admin(t, db)
	if _, e := db.Exec(`INSERT INTO auth_sessions(id,account_id,expires_at)VALUES(gen_random_uuid(),$1,now()+interval '1 day')`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO password_recoveries(account_id,challenge_id,token_digest,expires_at,last_sent_at)VALUES($1,gen_random_uuid(),decode(repeat('01',32),'hex'),now()+interval '1 hour',now())`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO password_recovery_outbox(id,account_id,kind,sealed_payload,expires_at,created_at,next_attempt_at)VALUES(gen_random_uuid(),$1,'changed','secret'::bytea,now()+interval '1 day',now(),now())`, id); e != nil {
		t.Fatal(e)
	}
	before, e := s.Status(ctx, id)
	if e != nil || before.Requested || before.PolicyVersion != PolicyVersion {
		t.Fatalf("before %+v %v", before, e)
	}
	r, e := s.Request(ctx, id, true, PolicyVersion, guard)
	if e != nil || r.Status != "requested" || r.Version != 1 {
		t.Fatalf("request %+v %v", r, e)
	}
	again, e := s.Request(ctx, id, true, PolicyVersion, guard)
	if e != nil || again.ID != r.ID {
		t.Fatalf("retry %+v %v", again, e)
	}
	var disabled, hidden, revoked, reset, mail bool
	e = db.QueryRow(`SELECT deletion_requested_at IS NOT NULL,NOT directory_visible,NOT EXISTS(SELECT 1 FROM auth_sessions WHERE account_id=$1 AND revoked_at IS NULL),NOT EXISTS(SELECT 1 FROM password_recoveries WHERE account_id=$1 AND token_digest IS NOT NULL),NOT EXISTS(SELECT 1 FROM password_recovery_outbox WHERE account_id=$1)FROM accounts WHERE id=$1`, id).Scan(&disabled, &hidden, &revoked, &reset, &mail)
	if e != nil || !disabled || !hidden || !revoked || !reset || !mail {
		t.Fatalf("atomic disabling %v %v %v %v %v %v", disabled, hidden, revoked, reset, mail, e)
	}
	st, e := s.Status(ctx, id)
	if !errors.Is(e, access.ErrForbidden) {
		t.Fatalf("pending account status must be denied %+v %v", st, e)
	}
	page, e := s.List(ctx, reviewer, "requested", "", 20, guard)
	if e != nil || len(page.Items) != 1 {
		t.Fatalf("list %+v %v", page, e)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "@") || strings.Contains(string(raw), "email") {
		t.Fatal("list leaks email")
	}
	d, e := s.Detail(ctx, reviewer, r.ID, guard)
	if e != nil || d.Email == "" {
		t.Fatalf("detail %+v %v", d, e)
	}
	reviewed, e := s.Review(ctx, reviewer, r.ID, 1, guard)
	if e != nil || reviewed.Status != "reviewing" || reviewed.Version != 2 {
		t.Fatalf("review %+v %v", reviewed, e)
	}
	if _, e = s.Review(ctx, reviewer, r.ID, 1, guard); !errors.Is(e, ErrConflict) {
		t.Fatal("stale version accepted", e)
	}
	var audit int
	if e = db.QueryRow(`SELECT count(*)FROM account_deletion_audit_events WHERE request_id=$1`, r.ID).Scan(&audit); e != nil || audit != 4 {
		t.Fatalf("audit %d %v", audit, e)
	}
}

func TestRequestGuardsAndAtomicRollback(t *testing.T) {
	db, s, id := fixture(t)
	ctx := context.Background()
	denied := errors.New("guard rejected")
	for _, in := range []struct {
		confirm bool
		policy  string
	}{{false, PolicyVersion}, {true, "old"}} {
		if _, e := s.Request(ctx, id, in.confirm, in.policy, guard); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	if _, e := s.Request(ctx, id, true, PolicyVersion, nil); !errors.Is(e, access.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Request(ctx, id, true, PolicyVersion, func(context.Context, *sql.Tx) error { return denied }); !errors.Is(e, denied) {
		t.Fatal(e)
	}
	if _, e := s.List(ctx, id, "requested", "", 20, guard); !errors.Is(e, access.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := db.Exec(`ALTER TABLE account_deletion_audit_events ADD CONSTRAINT test_reject_audit CHECK(action<>'requested')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Request(ctx, id, true, PolicyVersion, guard); e == nil {
		t.Fatal("audit failure must rollback")
	}
	var pending bool
	if e := db.QueryRow(`SELECT deletion_requested_at IS NOT NULL FROM accounts WHERE id=$1`, id).Scan(&pending); e != nil || pending {
		t.Fatalf("failed mutation persisted %v %v", pending, e)
	}
}

func TestLastAdministratorAndConcurrentRequests(t *testing.T) {
	db, s, _ := fixture(t)
	ctx := context.Background()
	a := admin(t, db)
	if _, e := s.Request(ctx, a, true, PolicyVersion, guard); !errors.Is(e, access.ErrLastAdministrator) {
		t.Fatalf("last admin %v", e)
	}
	b := admin(t, db)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{a, b} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, e := s.Request(ctx, id, true, PolicyVersion, guard); results <- e }(id)
	}
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !errors.Is(e, access.ErrLastAdministrator) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatalf("want one active admin preserved, winners=%d", wins)
	}
}

func TestActivePastoralCommitmentsBlockButPastRecordsRemain(t *testing.T) {
	db, s, member := fixture(t)
	ctx := context.Background()
	pastor := admin(t, db)
	admin(t, db)
	if _, e := db.Exec(`INSERT INTO pastor_profiles(account_id,bio,time_zone,listed)VALUES($1,'Synthetic','UTC',true)`, pastor); e != nil {
		t.Fatal(e)
	}
	var slot, booking string
	now := s.now()
	if e := db.QueryRow(`INSERT INTO pastor_slots(pastor_id,starts_at,ends_at)VALUES($1,$2,$3)RETURNING id`, pastor, now.Add(time.Hour), now.Add(2*time.Hour)).Scan(&slot); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO pastor_bookings(slot_id,pastor_id,member_id,starts_at,ends_at,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$6)RETURNING id`, slot, pastor, member, now.Add(time.Hour), now.Add(2*time.Hour), now).Scan(&booking); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{member, pastor} {
		if _, e := s.Request(ctx, id, true, PolicyVersion, guard); !errors.Is(e, ErrActiveCommitments) {
			t.Fatalf("active commitment %v", e)
		}
		var pending bool
		if e := db.QueryRow(`SELECT deletion_requested_at IS NOT NULL FROM accounts WHERE id=$1`, id).Scan(&pending); e != nil || pending {
			t.Fatal("blocked request disabled account", e)
		}
	}
	if _, e := db.Exec(`UPDATE pastor_bookings SET starts_at=$2,ends_at=$3 WHERE id=$1`, booking, now.Add(-2*time.Hour), now.Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Request(ctx, member, true, PolicyVersion, guard); e != nil {
		t.Fatal(e)
	}
	var retained bool
	if e := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pastor_bookings WHERE id=$1)`, booking).Scan(&retained); e != nil || !retained {
		t.Fatal("historical booking removed", e)
	}
}

func TestDeletionSerializesWithProtectedAppointment(t *testing.T) {
	db, s, member := fixture(t)
	ctx := context.Background()
	actor := admin(t, db)
	if _, e := db.Exec(`WITH s AS(INSERT INTO auth_sessions(id,account_id,expires_at)VALUES(gen_random_uuid(),$1,now()+interval '1 day')RETURNING id)INSERT INTO mfa_factors(account_id,id,sealed_secret,enrollment_session_id,pending_expires_at,confirmed_at)SELECT $1,gen_random_uuid(),'secret'::bytea,id,now(),now()FROM s`, member); e != nil {
		t.Fatal(e)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	deletion := make(chan error, 1)
	go func() {
		_, e := s.Request(ctx, member, true, PolicyVersion, func(context.Context, *sql.Tx) error { close(locked); <-release; return nil })
		deletion <- e
	}()
	<-locked
	appointment := make(chan error, 1)
	go func() {
		_, e := access.NewService(db).ChurchGrant(ctx, actor, member, access.Change{Role: "staff", Reason: "Synthetic appointment"}, guard)
		appointment <- e
	}()
	close(release)
	if e := <-deletion; e != nil {
		t.Fatal(e)
	}
	if e := <-appointment; !errors.Is(e, access.ErrInvalid) {
		t.Fatal("pending account appointed", e)
	}
}

func TestReviewGuardRunsAfterRequestLockAndReadAuditsFailClosed(t *testing.T) {
	db, s, id := fixture(t)
	ctx := context.Background()
	reviewer := admin(t, db)
	r, e := s.Request(ctx, id, true, PolicyVersion, guard)
	if e != nil {
		t.Fatal(e)
	}
	proofAfterLock := func(ctx context.Context, tx *sql.Tx) error {
		other, e := db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer other.Rollback()
		var found string
		e = other.QueryRowContext(ctx, `SELECT id FROM account_deletion_requests WHERE id=$1 FOR UPDATE NOWAIT`, r.ID).Scan(&found)
		var pgError *pgconn.PgError
		if !errors.As(e, &pgError) || pgError.Code != "55P03" {
			return errors.New("request row was not locked before sensitive guard")
		}
		return nil
	}
	if _, e = s.Review(ctx, reviewer, r.ID, 1, proofAfterLock); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`ALTER TABLE account_deletion_audit_events ADD CONSTRAINT test_no_contact_read CHECK(action<>'viewed')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Detail(ctx, reviewer, r.ID, guard); e == nil {
		t.Fatal("sensitive contact read succeeded without audit")
	}
	if _, e = s.Detail(ctx, reviewer, r.ID, nil); !errors.Is(e, access.ErrForbidden) {
		t.Fatal("nil sensitive guard accepted", e)
	}
}

func TestRestoreReenablesSignInWithoutRestoringDirectoryOrSessions(t *testing.T) {
	db, s, id := fixture(t)
	ctx := context.Background()
	reviewer := admin(t, db)
	if _, e := db.Exec(`INSERT INTO auth_sessions(id,account_id,expires_at)VALUES(gen_random_uuid(),$1,now()+interval '1 day')`, id); e != nil {
		t.Fatal(e)
	}
	r, e := s.Request(ctx, id, true, PolicyVersion, guard)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.Review(ctx, reviewer, r.ID, r.Version, guard)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := s.Restore(ctx, reviewer, r.ID, r.Version, "Member confirmed the request was submitted by mistake", guard)
	if e != nil || restored.Status != "restored" || restored.Version != r.Version+1 {
		t.Fatalf("restore %+v %v", restored, e)
	}
	var enabled, hidden, revoked bool
	if e = db.QueryRow(`SELECT deletion_requested_at IS NULL,NOT directory_visible,NOT EXISTS(SELECT 1 FROM auth_sessions WHERE account_id=$1 AND revoked_at IS NULL)FROM accounts WHERE id=$1`, id).Scan(&enabled, &hidden, &revoked); e != nil || !enabled || !hidden || !revoked {
		t.Fatalf("restore state enabled=%v hidden=%v revoked=%v err=%v", enabled, hidden, revoked, e)
	}
	status, e := s.Status(ctx, id)
	if e != nil || status.Requested || status.Request == nil || status.Request.Status != "restored" {
		t.Fatalf("restored status %+v %v", status, e)
	}
	newRequest, e := s.Request(ctx, id, true, PolicyVersion, guard)
	if e != nil || newRequest.ID == r.ID {
		t.Fatalf("new deletion request after restoration: %v", e)
	}
	var newestID string
	if e = db.QueryRow(`SELECT id FROM account_deletion_requests WHERE account_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, id).Scan(&newestID); e != nil || newestID != newRequest.ID {
		t.Fatalf("newest deletion request %s want %s: %v", newestID, newRequest.ID, e)
	}
	var reason string
	if e = db.QueryRow(`SELECT reason FROM account_deletion_audit_events WHERE request_id=$1 AND action='restored'`, r.ID).Scan(&reason); e != nil || reason != "Member confirmed the request was submitted by mistake" {
		t.Fatalf("restoration reason %q %v", reason, e)
	}
}

func TestRestoreRejectsBadReasonStaleVersionAndFailedAuditAtomically(t *testing.T) {
	db, s, id := fixture(t)
	ctx := context.Background()
	reviewer := admin(t, db)
	r, e := s.Request(ctx, id, true, PolicyVersion, guard)
	if e != nil {
		t.Fatal(e)
	}
	for _, reason := range []string{"", "no", strings.Repeat("a", 501)} {
		if _, e = s.Restore(ctx, reviewer, r.ID, r.Version, reason, guard); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted invalid reason %q: %v", reason, e)
		}
	}
	if _, e = s.Restore(ctx, reviewer, r.ID, r.Version+1, "Reviewed and approved", guard); !errors.Is(e, ErrConflict) {
		t.Fatalf("accepted stale version: %v", e)
	}
	if _, e = db.Exec(`ALTER TABLE account_deletion_audit_events ADD CONSTRAINT test_reject_restore CHECK(action<>'restored')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Restore(ctx, reviewer, r.ID, r.Version, "Reviewed and approved", guard); e == nil {
		t.Fatal("restoration succeeded without its audit")
	}
	var disabled bool
	if e = db.QueryRow(`SELECT deletion_requested_at IS NOT NULL FROM accounts WHERE id=$1`, id).Scan(&disabled); e != nil || !disabled {
		t.Fatalf("failed restoration changed account: disabled=%v err=%v", disabled, e)
	}
}

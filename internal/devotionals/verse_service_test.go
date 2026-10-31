package devotionals

import (
	"context"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"sync"
	"testing"
	"time"
)

func verseSample(date string) VerseContent {
	return VerseContent{DisplayDate: date, Text: "Synthetic verse text for tests.", ScriptureReference: "Test 1:1", Translation: "Test translation"}
}
func TestDailyVerseUsesFixedGMTPlusOneBoundary(t *testing.T) {
	db, s, editor, _ := fixture(t)
	ctx := context.Background()
	clock := time.Date(2026, 10, 31, 22, 59, 59, 0, time.UTC)
	s.now = func() time.Time { return clock }
	for _, date := range []string{"2026-10-31", "2026-11-01"} {
		v, err := s.CreateVerse(ctx, editor, verseSample(date))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.UpdateVerse(ctx, editor, v.ID, VerseUpdate{Version: 1, Status: str("published")}); err != nil {
			t.Fatal(err)
		}
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SET TIME ZONE 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	before, err := s.TodayVerse(ctx)
	if err != nil || before.Date != "2026-10-31" || before.TimeZone != "UTC+01:00" || before.Verse == nil || before.Verse.DisplayDate != before.Date {
		t.Fatal(before, err)
	}
	if before.NextDayAt.Format(time.RFC3339) != "2026-10-31T23:00:00Z" {
		t.Fatal("wrong midnight", before.NextDayAt)
	}
	clock = clock.Add(time.Second)
	after, err := s.TodayVerse(ctx)
	if err != nil || after.Date != "2026-11-01" || after.Verse == nil || after.Verse.DisplayDate != after.Date {
		t.Fatal(after, err)
	}
	clock = time.Date(2026, 11, 1, 23, 0, 0, 0, time.UTC)
	empty, err := s.TodayVerse(ctx)
	if err != nil || empty.Date != "2026-11-02" || empty.Verse != nil {
		t.Fatal("fell back to an old verse", empty, err)
	}
	for _, tc := range []struct{ now, want string }{{"2026-12-31T23:00:00Z", "2027-01-01"}, {"2028-02-28T23:00:00Z", "2028-02-29"}, {"2027-07-01T22:59:59Z", "2027-07-01"}} {
		clock, _ = time.Parse(time.RFC3339, tc.now)
		got, err := s.TodayVerse(ctx)
		if err != nil || got.Date != tc.want {
			t.Fatal("calendar boundary", got, err)
		}
	}
}
func TestDailyVersePublicationConflictPrivacyAndWithdrawal(t *testing.T) {
	db, s, editor, member := fixture(t)
	ctx := context.Background()
	date := "2026-11-01"
	if _, err := s.CreateVerse(ctx, member, verseSample(date)); !errors.Is(err, ErrForbidden) {
		t.Fatal("member edited verse", err)
	}
	first, err := s.CreateVerse(ctx, editor, verseSample(date))
	if err != nil {
		t.Fatal(err)
	}
	today, err := s.TodayVerse(ctx)
	if err != nil || today.Verse != nil {
		t.Fatal("draft exposed", today, err)
	}
	if _, err = s.GetVerseEditorial(ctx, member, first.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("member read verse draft", err)
	}
	second, err := s.CreateVerse(ctx, editor, verseSample(date))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := s.UpdateVerse(ctx, editor, id, VerseUpdate{Version: 1, Status: str("published")})
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrVerseDateConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("same-day publication race", wins, conflicts)
	}
	today, err = s.TodayVerse(ctx)
	if err != nil || today.Verse == nil {
		t.Fatal(today, err)
	}
	winner := today.Verse.ID
	if _, err = s.UpdateVerse(ctx, editor, winner, VerseUpdate{Version: 1, Text: str("Stale update")}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale overwrite", err)
	}
	if _, err = s.UpdateVerse(ctx, editor, winner, VerseUpdate{Version: 2, Status: str("draft")}); err != nil {
		t.Fatal(err)
	}
	today, err = s.TodayVerse(ctx)
	if err != nil || today.Verse != nil {
		t.Fatal("withdrawn verse exposed", today, err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetVerseEditorial(ctx, editor, first.ID); err != nil {
		t.Fatal("migration erased verse", err)
	}
	if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, editor); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateVerse(ctx, editor, winner, VerseUpdate{Version: 3, Status: str("published")}); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked editor published", err)
	}
}
func TestDailyVerseValidationPaginationAndAuditRollback(t *testing.T) {
	db, s, editor, _ := fixture(t)
	ctx := context.Background()
	for _, date := range []string{"2026-02-29", "2026-1-01", "2026-11-01T00:00:00Z", "0000-01-01", ""} {
		if _, err := s.CreateVerse(ctx, editor, verseSample(date)); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid date", date, err)
		}
	}
	for range 3 {
		if _, err := s.CreateVerse(ctx, editor, verseSample("2026-11-01")); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for range 3 {
		page, err := s.ListVersesEditorial(ctx, editor, Options{Limit: 1, Cursor: cursor})
		if err != nil || len(page.Items) != 1 || seen[page.Items[0].ID] {
			t.Fatal(page, err)
		}
		seen[page.Items[0].ID] = true
		cursor = page.NextCursor
	}
	if cursor != "" {
		t.Fatal("extra page")
	}
	v, err := s.CreateVerse(ctx, editor, verseSample("2026-11-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE FUNCTION reject_daily_verse_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$; CREATE TRIGGER reject_daily_verse_audit BEFORE INSERT ON daily_verse_audit_events FOR EACH ROW EXECUTE FUNCTION reject_daily_verse_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateVerse(ctx, editor, v.ID, VerseUpdate{Version: 1, Status: str("published")}); err == nil {
		t.Fatal("audit failure ignored")
	}
	kept, err := s.GetVerseEditorial(ctx, editor, v.ID)
	if err != nil || kept.Status != "draft" || kept.Version != 1 {
		t.Fatal("partial write", kept, err)
	}
}

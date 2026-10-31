package devotionals

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDailyVerseTextValidationAndDatedEdits(t *testing.T) {
	_, s, editor, _ := fixture(t)
	ctx := context.Background()
	for _, c := range []VerseContent{{DisplayDate: "2026-11-01", Text: " ", ScriptureReference: "Test 1:1"}, {DisplayDate: "2026-11-01", Text: strings.Repeat("x", 4001), ScriptureReference: "Test 1:1"}, {DisplayDate: "2026-11-01", Text: "Test", ScriptureReference: ""}, {DisplayDate: "2026-11-01", Text: "Test\x00", ScriptureReference: "Test 1:1"}, {DisplayDate: "2026-11-01", Text: "Test", ScriptureReference: "Test 1:1", Translation: strings.Repeat("x", 101)}} {
		if _, err := s.CreateVerse(ctx, editor, c); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad text accepted", err)
		}
	}
	c := verseSample("2026-11-01")
	c.Text = strings.Repeat("🙂", 4000)
	v, err := s.CreateVerse(ctx, editor, c)
	if err != nil {
		t.Fatal("Unicode limit", err)
	}
	edited, err := s.UpdateVerse(ctx, editor, v.ID, VerseUpdate{Version: 1, Text: str("Edited verse."), ScriptureReference: str("Test 2:1")})
	if err != nil || edited.Text != "Edited verse." {
		t.Fatal(edited, err)
	}
	if _, err = s.UpdateVerse(ctx, editor, v.ID, VerseUpdate{Version: 2, Status: str("published")}); err != nil {
		t.Fatal(err)
	}
	today, err := s.TodayVerse(ctx)
	if err != nil || today.Verse == nil || today.Verse.Text != "Edited verse." {
		t.Fatal(today, err)
	}
	if _, err = s.UpdateVerse(ctx, editor, v.ID, VerseUpdate{Version: 3, DisplayDate: str("2026-11-02")}); err != nil {
		t.Fatal(err)
	}
	today, err = s.TodayVerse(ctx)
	if err != nil || today.Verse != nil {
		t.Fatal("moved verse retained on old date", today, err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC) }
	today, err = s.TodayVerse(ctx)
	if err != nil || today.Verse == nil || today.Verse.ID != v.ID {
		t.Fatal("assigned date did not activate", today, err)
	}
}

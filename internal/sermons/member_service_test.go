package sermons

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemberProgressAndNotesArePrivateAndBounded(t *testing.T) {
	db, now := fixture(t)
	ctx := context.Background()
	var memberID, otherID string
	for _, row := range []struct {
		email  string
		target *string
	}{
		{"sermon-owner@example.invalid", &memberID},
		{"different-owner@example.invalid", &otherID},
	} {
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Sermon','Member',$2) RETURNING id`, row.email, now).Scan(row.target); err != nil {
			t.Fatal(err)
		}
	}
	published := now.Add(-time.Minute)
	sermonID := insert(t, db, "Member sermon", "Pastor", "Series", "Faith", &published)
	draftID := insert(t, db, "Private draft", "Pastor", "Series", "Faith", nil)
	service := NewMemberService(NewRepository(db), func() time.Time { return now })

	initial, err := service.GetProgress(ctx, memberID, sermonID)
	if err != nil || initial.PositionSeconds != 0 || initial.UpdatedAt != nil {
		t.Fatalf("initial progress: %+v %v", initial, err)
	}
	progress, err := service.SaveProgress(ctx, memberID, sermonID, 420)
	if err != nil || progress.PositionSeconds != 420 || progress.UpdatedAt == nil {
		t.Fatalf("save progress: %+v %v", progress, err)
	}
	progress, err = service.SaveProgress(ctx, memberID, sermonID, 12)
	if err != nil || progress.PositionSeconds != 12 {
		t.Fatalf("seeking backward must save: %+v %v", progress, err)
	}
	otherProgress, err := service.GetProgress(ctx, otherID, sermonID)
	if err != nil || otherProgress.PositionSeconds != 0 || otherProgress.UpdatedAt != nil {
		t.Fatalf("progress crossed accounts: %+v %v", otherProgress, err)
	}
	for _, position := range []int{-1, 86401} {
		if _, err := service.SaveProgress(ctx, memberID, sermonID, position); !errors.Is(err, ErrInvalidMemberInput) {
			t.Fatalf("invalid position %d accepted: %v", position, err)
		}
	}
	if _, err := service.GetProgress(ctx, memberID, draftID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft progress access: %v", err)
	}

	missing, err := service.GetNote(ctx, memberID, sermonID)
	if err != nil || missing != nil {
		t.Fatalf("missing note should be empty: %+v %v", missing, err)
	}
	note, err := service.SaveNote(ctx, memberID, sermonID, "  Notes stay private.  ")
	if err != nil || note.Body != "Notes stay private." {
		t.Fatalf("save note: %+v %v", note, err)
	}
	otherNote, err := service.GetNote(ctx, otherID, sermonID)
	if err != nil || otherNote != nil {
		t.Fatalf("note crossed accounts: %+v %v", otherNote, err)
	}
	if _, err := service.SaveNote(ctx, memberID, sermonID, "\n \t"); !errors.Is(err, ErrInvalidMemberInput) {
		t.Fatalf("blank note accepted: %v", err)
	}
	if _, err := service.SaveNote(ctx, memberID, sermonID, string(make([]byte, 0))); !errors.Is(err, ErrInvalidMemberInput) {
		t.Fatalf("empty note accepted: %v", err)
	}
	if _, err := service.SaveNote(ctx, memberID, sermonID, repeatRune('x', 10001)); !errors.Is(err, ErrInvalidMemberInput) {
		t.Fatalf("oversized note accepted: %v", err)
	}
	if _, err := service.GetNote(ctx, memberID, draftID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft note access: %v", err)
	}
	if err := service.DeleteNote(ctx, memberID, sermonID); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if err := service.DeleteNote(ctx, memberID, sermonID); err != nil {
		t.Fatalf("repeat delete should be safe: %v", err)
	}
	if got, err := service.GetNote(ctx, memberID, sermonID); err != nil || got != nil {
		t.Fatalf("deleted note visible: %+v %v", got, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.GetProgress(canceled, memberID, sermonID); err == nil {
		t.Fatal("canceled request was ignored")
	}
}

func repeatRune(r rune, count int) string {
	result := make([]rune, count)
	for i := range result {
		result[i] = r
	}
	return string(result)
}

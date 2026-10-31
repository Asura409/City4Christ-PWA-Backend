package devotionals

import (
	"context"
	"strings"
	"time"
)

func normalizeVerse(c VerseContent) (VerseContent, error) {
	c.DisplayDate = strings.TrimSpace(c.DisplayDate)
	date, err := time.Parse(time.DateOnly, c.DisplayDate)
	if err != nil || date.Year() < 1 || date.Year() > 9999 || date.Format(time.DateOnly) != c.DisplayDate {
		return VerseContent{}, invalid("displayDate", "Use a valid YYYY-MM-DD church date (GMT+1).")
	}
	for _, f := range []struct {
		value     *string
		name      string
		min, max  int
		multiline bool
	}{{&c.Text, "text", 1, 4000, true}, {&c.ScriptureReference, "scriptureReference", 1, 200, false}, {&c.Translation, "translation", 0, 100, false}} {
		*f.value, err = normalizeText(*f.value, f.name, f.min, f.max, f.multiline)
		if err != nil {
			return VerseContent{}, err
		}
	}
	return c, nil
}
func (s *Service) CreateVerse(ctx context.Context, actor string, c VerseContent) (Verse, error) {
	c, err := normalizeVerse(c)
	if err != nil {
		return Verse{}, err
	}
	return s.repo.createVerse(ctx, actor, c, s.now().UTC())
}
func (s *Service) UpdateVerse(ctx context.Context, actor, id string, u VerseUpdate) (Verse, error) {
	if !validID(id) {
		return Verse{}, ErrNotFound
	}
	if u.Version < 1 {
		return Verse{}, invalid("version", "Supply the current positive version.")
	}
	if u.DisplayDate == nil && u.Text == nil && u.ScriptureReference == nil && u.Translation == nil && u.Status == nil {
		return Verse{}, invalid("body", "Supply an editable field or status.")
	}
	if u.Status != nil && *u.Status != "draft" && *u.Status != "published" {
		return Verse{}, invalid("status", "Choose draft or published.")
	}
	return s.repo.updateVerse(ctx, actor, id, u, s.now().UTC())
}
func (s *Service) GetVerseEditorial(ctx context.Context, actor, id string) (Verse, error) {
	if !validID(id) {
		return Verse{}, ErrNotFound
	}
	return s.repo.verseEditorial(ctx, actor, id)
}
func (s *Service) ListVersesEditorial(ctx context.Context, actor string, o Options) (Page[Verse], error) {
	o, err := options(o)
	if err != nil {
		return Page[Verse]{}, err
	}
	return s.repo.listVersesEditorial(ctx, actor, o)
}
func (s *Service) TodayVerse(ctx context.Context) (TodayVerse, error) {
	now := s.now().UTC()
	local := now.In(churchTime)
	result := TodayVerse{Date: local.Format(time.DateOnly), TimeZone: churchTime.String(), ServerTime: now, NextDayAt: time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, churchTime).UTC()}
	if s.repo.db == nil {
		return TodayVerse{}, ErrUnavailable
	}
	e, err := scanVerse(s.repo.db.QueryRowContext(ctx, `SELECT `+verseColumns+` FROM daily_verses WHERE display_date=$1::date AND status='published' AND published_at<=$2`, result.Date, now))
	if err == ErrNotFound {
		return result, nil
	}
	if err != nil {
		return TodayVerse{}, err
	}
	result.Verse = &e.PublishedVerse
	return result, nil
}

package events

import (
	"context"
	"errors"

	ics "github.com/arran4/golang-ical"
)

var ErrCalendarUnavailable = errors.New("calendar export unavailable")

func (s *Service) Calendar(ctx context.Context, eventID string, viewers ...string) ([]byte, error) {
	if !validID(eventID) {
		return nil, ErrNotFound
	}
	viewer := ""
	if len(viewers) > 0 {
		viewer = viewers[0]
	}
	item, err := s.repo.Detail(ctx, eventID, viewer, s.now())
	if err != nil {
		return nil, err
	}
	calendar := ics.NewCalendarFor("City4Christ")
	calendar.SetMethod(ics.MethodPublish)
	calendar.SetXWRCalName("City4Christ Church Events")
	calendar.SetXWRTimezone(item.TimeZone)
	calendar.SetLastModified(item.UpdatedAt)
	entry := calendar.AddEvent(item.ID + "@city4christ.org")
	entry.SetCreatedTime(item.CreatedAt)
	entry.SetDtStampTime(s.now().UTC())
	entry.SetModifiedAt(item.UpdatedAt)
	entry.SetStartAt(item.StartsAt.UTC())
	entry.SetSummary(item.Title)
	entry.SetDescription(item.Description)
	entry.SetLocation(item.Location)
	entry.SetStatus(ics.ObjectStatusConfirmed)
	return []byte(calendar.Serialize(ics.WithNewLineWindows)), nil
}

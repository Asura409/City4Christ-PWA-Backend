package pastoral

import (
	"context"
	"time"
)

func (s *Service) CreateSlot(ctx context.Context, actor string, in SlotInput) (Slot, error) {
	now := s.now().UTC()
	if !validID(actor) || !in.StartsAt.After(now) || in.StartsAt.After(now.Add(180*24*time.Hour)) || in.EndsAt.Sub(in.StartsAt) < 15*time.Minute || in.EndsAt.Sub(in.StartsAt) > 2*time.Hour {
		return Slot{}, ErrInvalid
	}
	return s.repo.CreateSlot(ctx, actor, in, now)
}
func (s *Service) Availability(ctx context.Context, pastor string, from, to time.Time, o Options) (Page[Slot], error) {
	if !validID(pastor) || from.IsZero() || !to.After(from) || to.Sub(from) > 31*24*time.Hour || to.After(s.now().Add(180*24*time.Hour)) {
		return Page[Slot]{}, ErrInvalid
	}
	o, err := options(o)
	if err != nil {
		return Page[Slot]{}, err
	}
	items, err := s.repo.Availability(ctx, pastor, from, to, o, s.now().UTC())
	if err != nil {
		return Page[Slot]{}, err
	}
	return finishPage(items, o.Limit, func(p Slot) string { return p.ID }), nil
}

func (s *Service) OwnSlots(ctx context.Context, actor string, from, to time.Time, o Options) (Page[Slot], error) {
	if !validID(actor) || from.IsZero() || !to.After(from) || to.Sub(from) > 31*24*time.Hour || to.After(s.now().Add(180*24*time.Hour)) {
		return Page[Slot]{}, ErrInvalid
	}
	o, err := options(o)
	if err != nil {
		return Page[Slot]{}, err
	}
	items, err := s.repo.OwnSlots(ctx, actor, from, to, o, s.now().UTC())
	if err != nil {
		return Page[Slot]{}, err
	}
	return finishPage(items, o.Limit, func(p Slot) string { return p.ID }), nil
}

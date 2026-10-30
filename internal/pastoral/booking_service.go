package pastoral

import "context"

func (s *Service) Request(ctx context.Context, actor, slot string) (Booking, error) {
	if !validID(actor) || !validID(slot) {
		return Booking{}, ErrInvalid
	}
	return s.repo.Request(ctx, actor, slot, s.now().UTC())
}
func (s *Service) Bookings(ctx context.Context, actor string, assigned bool, o Options) (Page[Booking], error) {
	if !validID(actor) {
		return Page[Booking]{}, ErrInvalid
	}
	o, err := options(o)
	if err != nil {
		return Page[Booking]{}, err
	}
	items, err := s.repo.Bookings(ctx, actor, assigned, o)
	if err != nil {
		return Page[Booking]{}, err
	}
	return finishPage(items, o.Limit, func(b Booking) string { return b.ID }), nil
}
func (s *Service) Decide(ctx context.Context, actor, id string, in Decision) (Booking, error) {
	if !validID(actor) || !validID(id) || in.Version < 1 || (in.Status != "approved" && in.Status != "declined" && in.Status != "cancelled") {
		return Booking{}, ErrInvalid
	}
	return s.repo.Transition(ctx, actor, id, in, false, s.now().UTC())
}
func (s *Service) Cancel(ctx context.Context, actor, id string) (Booking, error) {
	if !validID(actor) || !validID(id) {
		return Booking{}, ErrInvalid
	}
	return s.repo.Transition(ctx, actor, id, Decision{Status: "cancelled"}, true, s.now().UTC())
}
func (s *Service) WithdrawSlot(ctx context.Context, actor, id string) error {
	if !validID(actor) || !validID(id) {
		return ErrInvalid
	}
	return s.repo.WithdrawSlot(ctx, actor, id, s.now().UTC())
}

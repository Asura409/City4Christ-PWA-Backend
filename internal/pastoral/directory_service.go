package pastoral

import (
	"context"
	"time"
)

func (s *Service) Appoint(ctx context.Context, actor string, in ProfileInput, guard Guard) (Profile, error) {
	if !validID(actor) || !validID(in.AccountID) || len(in.TimeZone) > 100 || (in.TimeZone == "" || in.TimeZone == "Local") {
		return Profile{}, ErrInvalid
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		return Profile{}, ErrInvalid
	}
	bio, err := cleanBio(in.Bio)
	if err != nil {
		return Profile{}, err
	}
	in.Bio = bio
	return s.repo.Appoint(ctx, actor, in, guard, s.now().UTC())
}
func (s *Service) Directory(ctx context.Context, o Options) (Page[Profile], error) {
	o, err := options(o)
	if err != nil {
		return Page[Profile]{}, err
	}
	items, err := s.repo.Directory(ctx, o)
	if err != nil {
		return Page[Profile]{}, err
	}
	return finishPage(items, o.Limit, func(p Profile) string { return p.ID }), nil
}
func (s *Service) OwnProfile(ctx context.Context, actor string) (Profile, error) {
	if !validID(actor) {
		return Profile{}, ErrInvalid
	}
	return s.repo.OwnProfile(ctx, actor)
}

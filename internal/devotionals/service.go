package devotionals

import "context"

func (s *Service) Create(ctx context.Context, actor string, c Content) (Entry, error) {
	c, err := normalize(c)
	if err != nil {
		return Entry{}, err
	}
	return s.repo.create(ctx, actor, c, s.now().UTC())
}
func (s *Service) Update(ctx context.Context, actor, id string, u Update) (Entry, error) {
	if !validID(id) {
		return Entry{}, ErrNotFound
	}
	if u.Version < 1 {
		return Entry{}, invalid("version", "Supply the current positive version.")
	}
	if u.Title == nil && u.Body == nil && u.ScriptureReference == nil && u.Author == nil && u.Status == nil {
		return Entry{}, invalid("body", "Supply an editable field or status.")
	}
	if u.Status != nil && *u.Status != "draft" && *u.Status != "published" {
		return Entry{}, invalid("status", "Choose draft or published.")
	}
	return s.repo.update(ctx, actor, id, u, s.now().UTC())
}
func (s *Service) GetPublished(ctx context.Context, id string) (PublishedEntry, error) {
	if !validID(id) {
		return PublishedEntry{}, ErrNotFound
	}
	return s.repo.published(ctx, id, s.now().UTC())
}
func (s *Service) GetEditorial(ctx context.Context, actor, id string) (Entry, error) {
	if !validID(id) {
		return Entry{}, ErrNotFound
	}
	return s.repo.editorial(ctx, actor, id)
}
func (s *Service) ListPublished(ctx context.Context, o Options) (Page[PublishedEntry], error) {
	o, err := options(o)
	if err != nil {
		return Page[PublishedEntry]{}, err
	}
	return s.repo.listPublished(ctx, o, s.now().UTC())
}
func (s *Service) ListEditorial(ctx context.Context, actor string, o Options) (Page[Entry], error) {
	o, err := options(o)
	if err != nil {
		return Page[Entry]{}, err
	}
	return s.repo.listEditorial(ctx, actor, o)
}

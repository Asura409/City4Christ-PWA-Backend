package bible

import (
	"context"
	"golang.org/x/sync/singleflight"
	"strings"
	"sync"
	"time"
)

type cached struct {
	passage Passage
	expires time.Time
}
type Service struct {
	provider Provider
	gate     Gate
	now      func() time.Time
	mu       sync.Mutex
	cache    map[string]cached
	pending  singleflight.Group
}

func NewService(p Provider, gate Gate, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{provider: p, gate: gate, now: now, cache: map[string]cached{}}
}
func (s *Service) get(key string) (Passage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.cache[key]
	if !ok || !s.now().Before(v.expires) {
		delete(s.cache, key)
		return Passage{}, false
	}
	return clone(v.passage), true
}
func (s *Service) put(key string, p Passage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.cache {
		if !s.now().Before(v.expires) {
			delete(s.cache, k)
		}
	}
	if len(s.cache) >= 128 {
		oldest := ""
		var expiry time.Time
		for k, v := range s.cache {
			if oldest == "" || v.expires.Before(expiry) {
				oldest, expiry = k, v.expires
			}
		}
		delete(s.cache, oldest)
	}
	s.cache[key] = cached{clone(p), s.now().Add(time.Hour)}
}
func (s *Service) Lookup(ctx context.Context, q Query) (Passage, error) {
	q, err := normalize(q)
	if err != nil {
		return Passage{}, err
	}
	if ctx.Err() != nil {
		return Passage{}, ctx.Err()
	}
	key := q.Translation + "|" + strings.ToLower(q.Reference)
	if p, ok := s.get(key); ok {
		return p, nil
	}
	result := s.pending.DoChan(key, func() (any, error) {
		if p, ok := s.get(key); ok {
			return p, nil
		}
		if s.provider == nil || s.gate == nil {
			return Passage{}, ErrUnavailable
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		allowed, err := s.gate(fetchCtx)
		if err != nil {
			return Passage{}, ErrUnavailable
		}
		if !allowed {
			return Passage{}, ErrRateLimited
		}
		p, err := s.provider.Lookup(fetchCtx, q)
		if err != nil {
			return Passage{}, err
		}
		if p.Translation.ID != q.Translation || len(p.Verses) == 0 || p.Text == "" {
			return Passage{}, ErrUnavailable
		}
		s.put(key, p)
		return p, nil
	})
	select {
	case <-ctx.Done():
		return Passage{}, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return Passage{}, r.Err
		}
		return clone(r.Val.(Passage)), nil
	}
}

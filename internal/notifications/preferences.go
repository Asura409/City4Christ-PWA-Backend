package notifications

import (
	"context"
	"time"
)

type Preferences struct {
	PushEnabled   bool `json:"pushEnabled"`
	Groups        bool `json:"groups"`
	Events        bool `json:"events"`
	Pastoral      bool `json:"pastoral"`
	Messages      bool `json:"messages"`
	Announcements bool `json:"announcements"`
}

func (s *Service) Preferences(ctx context.Context, actor string) (Preferences, error) {
	if !validID(actor) {
		return Preferences{}, ErrInvalid
	}
	p := Preferences{}
	err := s.repo.db.QueryRowContext(ctx, `SELECT COALESCE(p.push_enabled,false),COALESCE(p.groups,true),COALESCE(p.events,true),COALESCE(p.pastoral,true),COALESCE(p.messages,true),COALESCE(p.announcements,true) FROM accounts a LEFT JOIN notification_preferences p ON p.account_id=a.id WHERE a.id=$1 AND a.email_verified_at IS NOT NULL`, actor).Scan(&p.PushEnabled, &p.Groups, &p.Events, &p.Pastoral, &p.Messages, &p.Announcements)
	return p, err
}
func (s *Service) SetPreferences(ctx context.Context, actor string, p Preferences) (Preferences, error) {
	if !validID(actor) {
		return Preferences{}, ErrInvalid
	}
	result, err := s.repo.db.ExecContext(ctx, `INSERT INTO notification_preferences(account_id,push_enabled,groups,events,pastoral,messages,announcements) SELECT id,$2,$3,$4,$5,$6,$7 FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL ON CONFLICT(account_id) DO UPDATE SET push_enabled=EXCLUDED.push_enabled,groups=EXCLUDED.groups,events=EXCLUDED.events,pastoral=EXCLUDED.pastoral,messages=EXCLUDED.messages,announcements=EXCLUDED.announcements`, actor, p.PushEnabled, p.Groups, p.Events, p.Pastoral, p.Messages, p.Announcements)
	if err != nil {
		return Preferences{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Preferences{}, err
	}
	if n != 1 {
		return Preferences{}, ErrNotFound
	}
	return p, nil
}
func (s *Service) MarkAllRead(ctx context.Context, actor string) (int64, error) {
	if !validID(actor) {
		return 0, ErrInvalid
	}
	return s.repo.MarkAllRead(ctx, actor, s.now())
}
func (r *Repository) MarkAllRead(ctx context.Context, actor string, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE notifications SET read_at=$2 WHERE account_id=$1 AND read_at IS NULL AND c4c_group_content_visible(group_id,$1::uuid)`, actor, now.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

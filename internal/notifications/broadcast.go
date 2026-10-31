package notifications

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/groupaccess"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrForbidden = errors.New("notification action forbidden")
var ErrConflict = errors.New("notification state conflict")

type BroadcastInput struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Audience string `json:"audience"`
	GroupID  string `json:"groupId,omitempty"`
}
type Broadcast struct {
	ID string `json:"id"`
	BroadcastInput
	Status         string     `json:"status"`
	Version        int        `json:"version"`
	RecipientCount int        `json:"recipientCount"`
	CreatedAt      time.Time  `json:"createdAt"`
	SentAt         *time.Time `json:"sentAt"`
}

func broadcastText(v string, max int) bool {
	if !utf8.ValidString(v) || strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
func broadcastPermission(ctx context.Context, tx *sql.Tx, actor, group string) error {
	if !validID(actor) {
		return ErrForbidden
	}
	err := groupaccess.CheckTx(ctx, tx, actor, group, "broadcasts.send")
	if errors.Is(err, access.ErrForbidden) || errors.Is(err, groupaccess.ErrDenied) {
		return ErrForbidden
	}
	return err
}
func (s *Service) CreateBroadcast(ctx context.Context, actor string, in BroadcastInput) (Broadcast, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if !broadcastText(in.Title, 120) || !broadcastText(in.Body, 500) || (in.Audience != "church" && in.Audience != "group") || (in.Audience == "church" && in.GroupID != "") || (in.Audience == "group" && !validID(in.GroupID)) {
		return Broadcast{}, ErrInvalid
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return Broadcast{}, err
	}
	defer tx.Rollback()
	if err = broadcastPermission(ctx, tx, actor, in.GroupID); err != nil {
		return Broadcast{}, err
	}
	var b Broadcast
	b.BroadcastInput = in
	err = tx.QueryRowContext(ctx, `INSERT INTO notification_broadcasts(actor_id,title,body,group_id,created_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5) RETURNING id,status,version,created_at`, actor, in.Title, in.Body, in.GroupID, s.now()).Scan(&b.ID, &b.Status, &b.Version, &b.CreatedAt)
	if err != nil {
		return Broadcast{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO notification_broadcast_audit(broadcast_id,actor_id,action) VALUES($1,$2,'created')`, b.ID, actor); err != nil {
		return Broadcast{}, err
	}
	return b, tx.Commit()
}
func loadBroadcast(ctx context.Context, tx *sql.Tx, id string) (Broadcast, error) {
	var b Broadcast
	err := tx.QueryRowContext(ctx, `SELECT id,title,body,COALESCE(group_id::text,''),status,version,recipient_count,created_at,sent_at FROM notification_broadcasts WHERE id=$1 FOR UPDATE`, id).Scan(&b.ID, &b.Title, &b.Body, &b.GroupID, &b.Status, &b.Version, &b.RecipientCount, &b.CreatedAt, &b.SentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Broadcast{}, ErrNotFound
	}
	b.Audience = "church"
	if b.GroupID != "" {
		b.Audience = "group"
	}
	return b, err
}
func broadcastRecipients(ctx context.Context, tx *sql.Tx, group string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM accounts WHERE email_verified_at IS NOT NULL AND c4c_group_content_visible(NULLIF($1,'')::uuid,id) ORDER BY id LIMIT 10001`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) > 10000 {
		return nil, ErrInvalid
	}
	return ids, rows.Err()
}
func (s *Service) Broadcast(ctx context.Context, actor, id string) (Broadcast, error) {
	return s.broadcastAction(ctx, actor, id, 0, false)
}
func (s *Service) SendBroadcast(ctx context.Context, actor, id string, version int) (Broadcast, error) {
	if version < 1 {
		return Broadcast{}, ErrInvalid
	}
	return s.broadcastAction(ctx, actor, id, version, true)
}
func (s *Service) broadcastAction(ctx context.Context, actor, id string, version int, send bool) (Broadcast, error) {
	if !validID(id) {
		return Broadcast{}, ErrNotFound
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return Broadcast{}, err
	}
	defer tx.Rollback()
	b, err := loadBroadcast(ctx, tx, id)
	if err != nil {
		return Broadcast{}, err
	}
	if err = broadcastPermission(ctx, tx, actor, b.GroupID); err != nil {
		return Broadcast{}, err
	}
	if send && b.Version != version {
		return Broadcast{}, ErrConflict
	}
	if b.Status == "sent" {
		return b, tx.Commit()
	}
	ids, err := broadcastRecipients(ctx, tx, b.GroupID)
	if err != nil {
		return Broadcast{}, err
	}
	b.RecipientCount = len(ids)
	if !send {
		return b, tx.Commit()
	}
	path, kind := "/#/notifications", "church.broadcast"
	if b.GroupID != "" {
		path, kind = "/#/groups/"+b.GroupID, "group.broadcast"
	}
	for _, recipient := range ids {
		if _, err = s.repo.CreateTx(ctx, tx, CreateInput{AccountID: recipient, Kind: kind, Title: b.Title, Body: b.Body, ResourcePath: path, DedupKey: "broadcast:" + b.ID, GroupID: b.GroupID}); err != nil {
			return Broadcast{}, err
		}
	}
	now := s.now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE notification_broadcasts SET status='sent',recipient_count=$2,sent_at=$3 WHERE id=$1`, id, len(ids), now); err != nil {
		return Broadcast{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO notification_broadcast_audit(broadcast_id,actor_id,action,recipient_count) VALUES($1,$2,'sent',$3)`, id, actor, len(ids)); err != nil {
		return Broadcast{}, err
	}
	b.Status = "sent"
	b.SentAt = &now
	return b, tx.Commit()
}

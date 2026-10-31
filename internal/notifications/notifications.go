// Package notifications stores member-specific in-app notices.
package notifications

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrInvalid  = errors.New("invalid notification input")
	ErrNotFound = errors.New("notification not found")
)

type Notification struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	ResourcePath string     `json:"resourcePath,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	ReadAt       *time.Time `json:"readAt"`
	sequence     int64
}

type CreateInput struct {
	GroupID      string
	AccountID    string
	Kind         string
	Title        string
	Body         string
	ResourcePath string
	DedupKey     string
}

type Options struct {
	Limit  int
	Cursor string
}

type Page struct {
	Items      []Notification `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type Repository struct{ db *sql.DB }
type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func NewService(repo *Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func validInput(input CreateInput) bool {
	if (input.GroupID != "" && !validID(input.GroupID)) || !validID(input.AccountID) || len(input.Kind) < 2 || len(input.Kind) > 64 || input.Kind[0] < 'a' || input.Kind[0] > 'z' {
		return false
	}
	for _, char := range input.Kind[1:] {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("_.-", char)) {
			return false
		}
	}
	if !utf8.ValidString(input.Title) || strings.TrimSpace(input.Title) == "" || len([]rune(input.Title)) > 120 || !utf8.ValidString(input.Body) || len([]rune(input.Body)) > 500 || len(input.DedupKey) > 255 {
		return false
	}
	if input.ResourcePath != "" {
		u, err := url.Parse(input.ResourcePath)
		if err != nil || u.Scheme != "" || u.Host != "" || u.Path != "/" || !strings.HasPrefix(input.ResourcePath, "/#/") || !strings.HasPrefix(u.Fragment, "/") || strings.ContainsAny(u.Fragment, "?#\\") || len(input.ResourcePath) > 500 {
			return false
		}
	}
	return true
}

func (r *Repository) CreateTx(ctx context.Context, tx *sql.Tx, input CreateInput) (Notification, error) {
	if tx == nil || !validInput(input) {
		return Notification{}, ErrInvalid
	}
	query := `INSERT INTO notifications(account_id,kind,title,body,resource_path,dedupe_key,created_at,group_id)
		VALUES($1,$2,$3,$4,$5,NULLIF($6,''),now(),NULLIF($7,'')::uuid)
		ON CONFLICT (account_id,dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		RETURNING sequence,id,kind,title,body,resource_path,created_at,read_at`
	notice, err := scanNotification(tx.QueryRowContext(ctx, query, input.AccountID, input.Kind, strings.TrimSpace(input.Title), strings.TrimSpace(input.Body), input.ResourcePath, input.DedupKey, input.GroupID))
	if err == nil {
		if err = queuePushTx(ctx, tx, notice.ID); err != nil {
			return Notification{}, err
		}
		return notice, nil
	}
	if !errors.Is(err, sql.ErrNoRows) || input.DedupKey == "" {
		return Notification{}, err
	}
	return scanNotification(tx.QueryRowContext(ctx, `SELECT sequence,id,kind,title,body,resource_path,created_at,read_at FROM notifications WHERE account_id=$1 AND dedupe_key=$2`, input.AccountID, input.DedupKey))
}

func (r *Repository) Create(ctx context.Context, input CreateInput) (Notification, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Notification{}, err
	}
	defer tx.Rollback()
	notice, err := r.CreateTx(ctx, tx, input)
	if err != nil {
		return Notification{}, err
	}
	if err := tx.Commit(); err != nil {
		return Notification{}, err
	}
	return notice, nil
}

func (r *Repository) List(ctx context.Context, accountID string, options Options) (Page, error) {
	limit := options.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 || !validID(accountID) {
		return Page{}, ErrInvalid
	}
	var before any
	if options.Cursor != "" {
		sequence, err := strconv.ParseInt(options.Cursor, 10, 64)
		if err != nil || sequence < 1 {
			return Page{}, ErrInvalid
		}
		before = sequence
	}
	rows, err := r.db.QueryContext(ctx, `SELECT sequence,id,kind,title,body,resource_path,created_at,read_at FROM notifications
		WHERE account_id=$1 AND c4c_group_content_visible(group_id,$1::uuid) AND ($2::bigint IS NULL OR sequence<$2) ORDER BY sequence DESC LIMIT $3`, accountID, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Items: make([]Notification, 0, limit)}
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = strconv.FormatInt(page.Items[len(page.Items)-1].sequence, 10)
	}
	return page, nil
}

func (r *Repository) UnreadCount(ctx context.Context, accountID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE account_id=$1 AND read_at IS NULL AND c4c_group_content_visible(group_id,$1::uuid)`, accountID).Scan(&count)
	return count, err
}

func (r *Repository) MarkRead(ctx context.Context, accountID, notificationID string, now time.Time) (bool, error) {
	if !validID(accountID) || !validID(notificationID) {
		return false, ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `UPDATE notifications SET read_at=$3 WHERE account_id=$1 AND id=$2 AND read_at IS NULL AND c4c_group_content_visible(group_id,$1::uuid)`, accountID, notificationID, now.UTC())
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed == 1 {
		return true, nil
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE account_id=$1 AND id=$2 AND c4c_group_content_visible(group_id,$1::uuid))`, accountID, notificationID).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, ErrNotFound
	}
	return false, nil
}

func scanNotification(row interface{ Scan(...any) error }) (Notification, error) {
	var item Notification
	var resourcePath string
	var readAt sql.NullTime
	err := row.Scan(&item.sequence, &item.ID, &item.Kind, &item.Title, &item.Body, &resourcePath, &item.CreatedAt, &readAt)
	if err != nil {
		return Notification{}, err
	}
	item.ResourcePath = resourcePath
	if readAt.Valid {
		item.ReadAt = &readAt.Time
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, accountID string, options Options) (Page, error) {
	if !validID(accountID) {
		return Page{}, ErrInvalid
	}
	return s.repo.List(ctx, accountID, options)
}

func (s *Service) UnreadCount(ctx context.Context, accountID string) (int, error) {
	if !validID(accountID) {
		return 0, ErrInvalid
	}
	return s.repo.UnreadCount(ctx, accountID)
}

func (s *Service) MarkRead(ctx context.Context, accountID, notificationID string) (bool, error) {
	if !validID(accountID) || !validID(notificationID) {
		return false, ErrInvalid
	}
	return s.repo.MarkRead(ctx, accountID, notificationID, s.now())
}

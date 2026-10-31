// Package posts implements the authenticated church-wide member feed.
package posts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
	"github.com/google/uuid"
)

var (
	ErrInvalid  = errors.New("invalid post or feed options")
	ErrNotFound = errors.New("post not found")
)

type Post struct {
	GroupID      string             `json:"groupId,omitempty"`
	ID           string             `json:"id"`
	AuthorID     string             `json:"authorId"`
	FirstName    string             `json:"firstName"`
	LastName     string             `json:"lastName"`
	Body         string             `json:"body"`
	CreatedAt    time.Time          `json:"createdAt"`
	Attachments  []media.Attachment `json:"attachments"`
	AmenCount    int                `json:"amenCount"`
	Reacted      bool               `json:"reacted"`
	CommentCount int                `json:"commentCount"`
}

type ReactionState struct {
	PostID    string `json:"postId"`
	AmenCount int    `json:"amenCount"`
	Reacted   bool   `json:"reacted"`
}

type CreateInput struct {
	GroupID       string   `json:"groupId,omitempty"`
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
}

type Options struct {
	GroupID string
	Limit   int
	Cursor  string
}

type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type Page struct {
	Items      []Post `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, author string, in CreateInput, signer media.DownloadSigner) (Post, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Post{}, err
	}
	defer tx.Rollback()
	if in.GroupID != "" {
		var visible bool
		if err = tx.QueryRowContext(ctx, `SELECT c4c_group_content_visible($1::uuid,$2::uuid)`, in.GroupID, author).Scan(&visible); err != nil {
			return Post{}, err
		}
		if !visible {
			return Post{}, ErrNotFound
		}
	}
	attachments, err := media.LoadAttachableTx(ctx, tx, author, string(media.PurposePostAttachment), in.AttachmentIDs)
	if err != nil {
		if errors.Is(err, media.ErrInvalidAttachment) {
			return Post{}, ErrInvalid
		}
		return Post{}, err
	}
	attachments, err = media.SignAttachments(ctx, signer, attachments)
	if err != nil {
		return Post{}, err
	}
	p, err := scan(tx.QueryRowContext(ctx, `WITH created AS (
 INSERT INTO church_posts(author_id,body,group_id) VALUES($1,$2,NULLIF($3,'')::uuid)
 RETURNING id,author_id,body,created_at,group_id
)
SELECT p.id,p.author_id,a.first_name,a.last_name,p.body,p.created_at,0::bigint,false,0::bigint,COALESCE(p.group_id::text,'')
FROM created p JOIN accounts a ON a.id=p.author_id`, author, in.Body, in.GroupID))
	if err != nil {
		return Post{}, err
	}
	if err = insertAttachments(ctx, tx, "post_media_attachments", "post_id", p.ID, attachments); err != nil {
		return Post{}, err
	}
	p.Attachments = attachments
	if err = tx.Commit(); err != nil {
		return Post{}, err
	}
	return p, nil
}

func insertAttachments(ctx context.Context, tx *sql.Tx, table, parentColumn, parentID string, attachments []media.Attachment) error {
	for i, item := range attachments {
		query := "INSERT INTO " + table + "(" + parentColumn + ",upload_id,position) VALUES($1,$2,$3)"
		if _, err := tx.ExecContext(ctx, query, parentID, item.ID, i); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) List(ctx context.Context, o Options, after *Cursor, viewerID string) ([]Post, error) {
	var at *time.Time
	var id *string
	if after != nil {
		at, id = &after.CreatedAt, &after.ID
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.author_id,a.first_name,a.last_name,p.body,p.created_at,
 (SELECT count(*) FROM post_reactions pr WHERE pr.post_id=p.id AND pr.reaction='amen'),
 EXISTS(SELECT 1 FROM post_reactions pr WHERE pr.post_id=p.id AND pr.account_id=NULLIF($4,'')::uuid AND pr.reaction='amen'),
 (SELECT count(*) FROM post_comments pc WHERE pc.post_id=p.id AND pc.deleted_at IS NULL),COALESCE(p.group_id::text,'')
 FROM church_posts p JOIN accounts a ON a.id=p.author_id
 WHERE p.deleted_at IS NULL AND COALESCE(p.group_id::text,'')=$5 AND c4c_group_content_visible(p.group_id,NULLIF($4,'')::uuid) AND ($1::timestamptz IS NULL OR (p.created_at,p.id)<($1::timestamptz,$2::uuid))
	ORDER BY p.created_at DESC,p.id DESC LIMIT $3`, at, id, o.Limit+1, viewerID, o.GroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Post, 0)
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		attachments, err := loadPostAttachments(ctx, r.db, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Attachments = attachments
	}
	return items, nil
}

func loadPostAttachments(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, postID string) ([]media.Attachment, error) {
	rows, err := q.QueryContext(ctx, `SELECT u.id::text,u.actual_content_type,u.actual_size,u.available_object_key
		FROM post_media_attachments a JOIN media_uploads u ON u.id=a.upload_id WHERE a.post_id=$1 ORDER BY a.position`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]media.Attachment, 0)
	for rows.Next() {
		var item media.Attachment
		if err := rows.Scan(&item.ID, &item.ContentType, &item.Size, &item.ObjectKey); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scan(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.FirstName, &p.LastName, &p.Body, &p.CreatedAt, &p.AmenCount, &p.Reacted, &p.CommentCount, &p.GroupID)
	p.CreatedAt = p.CreatedAt.UTC()
	return p, err
}

type Service struct {
	repo   *Repository
	signer media.DownloadSigner
}

func NewService(repo *Repository, signers ...media.DownloadSigner) *Service {
	s := &Service{repo: repo}
	if len(signers) > 0 {
		s.signer = signers[0]
	}
	return s
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id != uuid.Nil
}

func normalizePostBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if !utf8.ValidString(body) || len([]rune(body)) < 1 || len([]rune(body)) > 5000 || strings.ContainsRune(body, 0) {
		return "", ErrInvalid
	}
	return body, nil
}

func (s *Service) Create(ctx context.Context, author string, in CreateInput) (Post, error) {
	var err error
	in.Body, err = normalizePostBody(in.Body)
	if !validID(author) || err != nil || (in.GroupID != "" && !validID(in.GroupID)) {
		return Post{}, ErrInvalid
	}
	if media.ValidateAttachmentIDs(in.AttachmentIDs, media.MaxPostAttachments) != nil {
		return Post{}, ErrInvalid
	}
	return s.repo.Create(ctx, author, in, s.signer)
}

func (s *Service) List(ctx context.Context, o Options, viewer ...string) (Page, error) {
	viewerID := ""
	if len(viewer) > 0 {
		viewerID = viewer[0]
		if !validID(viewerID) {
			return Page{}, ErrInvalid
		}
	}
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 || len(o.Cursor) > 2048 || (o.GroupID != "" && !validID(o.GroupID)) {
		return Page{}, ErrInvalid
	}
	var after *Cursor
	if o.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(o.Cursor)
		if err != nil {
			return Page{}, ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var c *Cursor
		if decoder.Decode(&c) != nil || c == nil || decoder.Decode(new(any)) != io.EOF || c.CreatedAt.IsZero() || !validID(c.ID) {
			return Page{}, ErrInvalid
		}
		after = c
	}
	if err := s.repo.checkGroup(ctx, viewerID, o.GroupID); err != nil {
		return Page{}, err
	}
	items, err := s.repo.List(ctx, o, after, viewerID)
	if err != nil {
		return Page{}, err
	}
	for i := range items {
		items[i].Attachments, err = media.SignAttachments(ctx, s.signer, items[i].Attachments)
		if err != nil {
			return Page{}, err
		}
	}
	page := Page{Items: items}
	if len(items) > o.Limit {
		page.Items = items[:o.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (r *Repository) SetAmen(ctx context.Context, actorID, postID string) (ReactionState, error) {
	return r.changeAmen(ctx, actorID, postID, true)
}

func (r *Repository) RemoveAmen(ctx context.Context, actorID, postID string) (ReactionState, error) {
	return r.changeAmen(ctx, actorID, postID, false)
}

func (r *Repository) changeAmen(ctx context.Context, actorID, postID string, add bool) (ReactionState, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ReactionState{}, err
	}
	defer tx.Rollback()
	if err = visiblePostTx(ctx, tx, actorID, postID); err != nil {
		return ReactionState{}, err
	}
	var exists string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM church_posts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, postID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ReactionState{}, ErrNotFound
	} else if err != nil {
		return ReactionState{}, err
	}
	if add {
		_, err = tx.ExecContext(ctx, `INSERT INTO post_reactions(post_id,account_id,reaction) VALUES($1,$2,'amen') ON CONFLICT(post_id,account_id) DO NOTHING`, postID, actorID)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM post_reactions WHERE post_id=$1 AND account_id=$2 AND reaction='amen'`, postID, actorID)
	}
	if err != nil {
		return ReactionState{}, err
	}
	state := ReactionState{PostID: postID}
	err = tx.QueryRowContext(ctx, `SELECT count(*), EXISTS(SELECT 1 FROM post_reactions WHERE post_id=$1 AND account_id=$2 AND reaction='amen') FROM post_reactions WHERE post_id=$1 AND reaction='amen'`, postID, actorID).Scan(&state.AmenCount, &state.Reacted)
	if err != nil {
		return ReactionState{}, err
	}
	if err = tx.Commit(); err != nil {
		return ReactionState{}, err
	}
	return state, nil
}

func (s *Service) SetAmen(ctx context.Context, actorID, postID string) (ReactionState, error) {
	if !validID(actorID) || !validID(postID) {
		return ReactionState{}, ErrInvalid
	}
	return s.repo.SetAmen(ctx, actorID, postID)
}

func (s *Service) RemoveAmen(ctx context.Context, actorID, postID string) (ReactionState, error) {
	if !validID(actorID) || !validID(postID) {
		return ReactionState{}, ErrInvalid
	}
	return s.repo.RemoveAmen(ctx, actorID, postID)
}

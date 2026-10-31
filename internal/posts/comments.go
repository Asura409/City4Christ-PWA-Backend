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

	"github.com/google/uuid"
)

var (
	ErrCommentInvalid  = errors.New("invalid comment or pagination options")
	ErrCommentNotFound = errors.New("comment or post not found")
)

type Comment struct {
	ID        string    `json:"id"`
	PostID    string    `json:"postId"`
	AuthorID  string    `json:"authorId"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type CommentInput struct {
	Body string `json:"body"`
}

type CommentOptions struct {
	Limit  int
	Cursor string
}

type CommentPage struct {
	Items      []Comment `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type commentCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type CommentRepository struct{ db *sql.DB }

func NewCommentRepository(db *sql.DB) *CommentRepository { return &CommentRepository{db: db} }

func (r *CommentRepository) Create(ctx context.Context, authorID, postID, body string) (Comment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback()
	if err = visiblePostTx(ctx, tx, authorID, postID); err != nil {
		if err == ErrNotFound {
			return Comment{}, ErrCommentNotFound
		}
		return Comment{}, err
	}
	var exists string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM church_posts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, postID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return Comment{}, ErrCommentNotFound
	} else if err != nil {
		return Comment{}, err
	}
	comment, err := scanComment(tx.QueryRowContext(ctx, `WITH created AS (
 INSERT INTO post_comments(post_id,account_id,body) VALUES($1,$2,$3)
 RETURNING id,post_id,account_id,body,created_at
)
SELECT c.id::text,c.post_id::text,c.account_id::text,a.first_name,a.last_name,c.body,c.created_at
FROM created c JOIN accounts a ON a.id=c.account_id`, postID, authorID, body))
	if err != nil {
		return Comment{}, err
	}
	if err = tx.Commit(); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (r *CommentRepository) List(ctx context.Context, postID string, options CommentOptions, after *commentCursor, viewers ...string) ([]Comment, error) {
	viewer := ""
	if len(viewers) > 0 {
		viewer = viewers[0]
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM church_posts WHERE id=$1 AND deleted_at IS NULL AND c4c_group_content_visible(group_id,NULLIF($2,'')::uuid))`, postID, viewer).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCommentNotFound
	}
	var createdAt *time.Time
	var id *string
	if after != nil {
		createdAt, id = &after.CreatedAt, &after.ID
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.id::text,c.post_id::text,c.account_id::text,a.first_name,a.last_name,c.body,c.created_at
		FROM post_comments c JOIN accounts a ON a.id=c.account_id JOIN church_posts p ON p.id=c.post_id
		WHERE c.post_id=$1 AND c.deleted_at IS NULL AND p.deleted_at IS NULL AND c4c_group_content_visible(p.group_id,NULLIF($5,'')::uuid)
		AND ($2::timestamptz IS NULL OR (c.created_at,c.id)<($2::timestamptz,$3::uuid))
		ORDER BY c.created_at DESC,c.id DESC LIMIT $4`, postID, createdAt, id, options.Limit+1, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Comment, 0)
	for rows.Next() {
		item, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CommentRepository) Delete(ctx context.Context, authorID, postID, commentID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE post_comments SET body='[Comment deleted]',deleted_at=now()
		WHERE id=$1 AND post_id=$2 AND account_id=$3 AND deleted_at IS NULL AND EXISTS(
		SELECT 1 FROM church_posts p WHERE p.id=post_comments.post_id AND p.deleted_at IS NULL AND c4c_group_content_visible(p.group_id,$3::uuid))`, commentID, postID, authorID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrCommentNotFound
	}
	return nil
}

func scanComment(row interface{ Scan(...any) error }) (Comment, error) {
	var item Comment
	err := row.Scan(&item.ID, &item.PostID, &item.AuthorID, &item.FirstName, &item.LastName, &item.Body, &item.CreatedAt)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, err
}

type CommentService struct{ repo *CommentRepository }

func NewCommentService(repo *CommentRepository) *CommentService { return &CommentService{repo: repo} }

func commentID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id != uuid.Nil
}

func (s *CommentService) Create(ctx context.Context, authorID, postID string, input CommentInput) (Comment, error) {
	input.Body = strings.TrimSpace(input.Body)
	if !commentID(authorID) || !commentID(postID) || !utf8.ValidString(input.Body) || len([]rune(input.Body)) < 1 || len([]rune(input.Body)) > 2000 || strings.ContainsRune(input.Body, 0) {
		return Comment{}, ErrCommentInvalid
	}
	return s.repo.Create(ctx, authorID, postID, input.Body)
}

func (s *CommentService) List(ctx context.Context, viewerID, postID string, options CommentOptions) (CommentPage, error) {
	if !commentID(viewerID) || !commentID(postID) || len(options.Cursor) > 2048 {
		return CommentPage{}, ErrCommentInvalid
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 {
		return CommentPage{}, ErrCommentInvalid
	}
	var after *commentCursor
	if options.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil {
			return CommentPage{}, ErrCommentInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var cursor *commentCursor
		if decoder.Decode(&cursor) != nil || cursor == nil || decoder.Decode(new(any)) != io.EOF || cursor.CreatedAt.IsZero() || !commentID(cursor.ID) {
			return CommentPage{}, ErrCommentInvalid
		}
		after = cursor
	}
	items, err := s.repo.List(ctx, postID, options, after, viewerID)
	if err != nil {
		return CommentPage{}, err
	}
	page := CommentPage{Items: items}
	if len(items) > options.Limit {
		page.Items = items[:options.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(commentCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return CommentPage{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (s *CommentService) Delete(ctx context.Context, authorID, postID, commentIDValue string) error {
	if !commentID(authorID) || !commentID(postID) || !commentID(commentIDValue) {
		return ErrCommentInvalid
	}
	return s.repo.Delete(ctx, authorID, postID, commentIDValue)
}

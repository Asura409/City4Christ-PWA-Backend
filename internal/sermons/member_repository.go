package sermons

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Progress struct {
	SermonID        string     `json:"sermonId"`
	PositionSeconds int        `json:"positionSeconds"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

type Note struct {
	SermonID  string    `json:"sermonId"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (r *Repository) GetProgress(ctx context.Context, accountID, sermonID string, now time.Time) (Progress, error) {
	var progress Progress
	var updated sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT s.id, COALESCE(p.position_seconds,0), p.updated_at
		FROM sermons s LEFT JOIN sermon_playback_progress p ON p.sermon_id=s.id AND p.account_id=$1
		WHERE s.id=$2 AND s.published_at <= $3`, accountID, sermonID, now.UTC()).Scan(&progress.SermonID, &progress.PositionSeconds, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, ErrNotFound
	}
	if err != nil {
		return Progress{}, err
	}
	if updated.Valid {
		value := updated.Time.UTC()
		progress.UpdatedAt = &value
	}
	return progress, nil
}

func (r *Repository) SaveProgress(ctx context.Context, accountID, sermonID string, position int, now time.Time) (Progress, error) {
	var progress Progress
	var updated time.Time
	err := r.db.QueryRowContext(ctx, `INSERT INTO sermon_playback_progress(account_id,sermon_id,position_seconds,updated_at)
		SELECT $1,s.id,$3,$4 FROM sermons s WHERE s.id=$2 AND s.published_at <= $4
		ON CONFLICT(account_id,sermon_id) DO UPDATE SET position_seconds=EXCLUDED.position_seconds,updated_at=EXCLUDED.updated_at
		RETURNING sermon_id,position_seconds,updated_at`, accountID, sermonID, position, now.UTC()).Scan(&progress.SermonID, &progress.PositionSeconds, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, ErrNotFound
	}
	if err != nil {
		return Progress{}, err
	}
	updated = updated.UTC()
	progress.UpdatedAt = &updated
	return progress, nil
}

func (r *Repository) GetNote(ctx context.Context, accountID, sermonID string, now time.Time) (*Note, error) {
	var id string
	var body sql.NullString
	var updated sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT s.id,n.body,n.updated_at
		FROM sermons s LEFT JOIN sermon_notes n ON n.sermon_id=s.id AND n.account_id=$1
		WHERE s.id=$2 AND s.published_at <= $3`, accountID, sermonID, now.UTC()).Scan(&id, &body, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !body.Valid {
		return nil, nil
	}
	return &Note{SermonID: id, Body: body.String, UpdatedAt: updated.Time.UTC()}, nil
}

func (r *Repository) SaveNote(ctx context.Context, accountID, sermonID, body string, now time.Time) (Note, error) {
	var note Note
	err := r.db.QueryRowContext(ctx, `INSERT INTO sermon_notes(account_id,sermon_id,body,updated_at)
		SELECT $1,s.id,$3,$4 FROM sermons s WHERE s.id=$2 AND s.published_at <= $4
		ON CONFLICT(account_id,sermon_id) DO UPDATE SET body=EXCLUDED.body,updated_at=EXCLUDED.updated_at
		RETURNING sermon_id,body,updated_at`, accountID, sermonID, body, now.UTC()).Scan(&note.SermonID, &note.Body, &note.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	note.UpdatedAt = note.UpdatedAt.UTC()
	return note, nil
}

func (r *Repository) DeleteNote(ctx context.Context, accountID, sermonID string, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sermons WHERE id=$1 AND published_at <= $2)`, sermonID, now.UTC()).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sermon_notes WHERE account_id=$1 AND sermon_id=$2`, accountID, sermonID); err != nil {
		return err
	}
	return tx.Commit()
}

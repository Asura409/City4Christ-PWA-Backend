package events

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/groupaccess"
)

func (s *Service) checkGroupManager(ctx context.Context, actor, groupID, eventID string, action func(*sql.Tx) error) error {
	if !validID(actor) {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if eventID != "" {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(group_id::text,'') FROM church_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&groupID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	if err = groupaccess.CheckTx(ctx, tx, actor, groupID, "events.manage"); err != nil {
		if errors.Is(err, access.ErrForbidden) || errors.Is(err, groupaccess.ErrDenied) {
			return ErrForbidden
		}
		return err
	}
	if err = action(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) GroupList(ctx context.Context, actor, groupID string, admin bool) ([]Event, error) {
	if !validID(groupID) || !validID(actor) {
		return nil, ErrInvalid
	}
	var result []Event
	if admin {
		err := s.checkGroupManager(ctx, actor, groupID, "", func(tx *sql.Tx) error {
			var err error
			result, err = s.repo.listTx(ctx, tx, actor, s.now(), true, groupID)
			return err
		})
		return result, err
	}
	var visible bool
	if err := s.db.QueryRowContext(ctx, `SELECT c4c_group_content_visible($1::uuid,$2::uuid)`, groupID, actor).Scan(&visible); err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err = s.repo.listTx(ctx, tx, actor, s.now(), false, groupID)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

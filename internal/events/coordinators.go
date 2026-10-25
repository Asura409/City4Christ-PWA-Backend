package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

func (s *Service) AssignCoordinator(ctx context.Context, actor, eventID, groupID, accountID string) (Coordinator, error) {
	if !validID(actor) || !validID(eventID) || !validID(groupID) || !validID(accountID) {
		return Coordinator{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Coordinator{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM church_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Coordinator{}, ErrNotFound
		}
		return Coordinator{}, err
	}
	if status == "cancelled" {
		return Coordinator{}, ErrConflict
	}
	var audience string
	var archived sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT audience,archived_at FROM groups WHERE id=$1 FOR SHARE`, groupID).Scan(&audience, &archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Coordinator{}, ErrForbidden
		}
		return Coordinator{}, err
	}
	if audience != "church" || archived.Valid {
		return Coordinator{}, ErrForbidden
	}
	if err = checkCoordinatorScope(ctx, tx, actor, groupID); err != nil {
		return Coordinator{}, err
	}
	var verified bool
	var membershipStatus string
	err = tx.QueryRowContext(ctx, `SELECT a.email_verified_at IS NOT NULL,gm.status
		FROM accounts a JOIN group_memberships gm ON gm.account_id=a.id
		WHERE a.id=$1 AND gm.group_id=$2 FOR UPDATE OF gm`, accountID, groupID).Scan(&verified, &membershipStatus)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!verified || membershipStatus != "active") {
		return Coordinator{}, ErrForbidden
	}
	if err != nil {
		return Coordinator{}, err
	}
	item, err := s.repo.assignCoordinatorTx(ctx, tx, actor, eventID, groupID, accountID, s.now())
	if err != nil {
		return Coordinator{}, err
	}
	if err = tx.Commit(); err != nil {
		return Coordinator{}, err
	}
	return item, nil
}

func checkCoordinatorScope(ctx context.Context, tx *sql.Tx, actor, groupID string) error {
	if err := access.CheckTx(ctx, tx, actor, "events.manage", ""); err == nil {
		return nil
	} else if !errors.Is(err, access.ErrForbidden) {
		return err
	}
	if err := access.CheckTx(ctx, tx, actor, "ministry.members.manage", groupID); err == nil {
		return nil
	} else if errors.Is(err, access.ErrForbidden) {
		return ErrForbidden
	} else {
		return err
	}
}

func (s *Service) RemoveCoordinator(ctx context.Context, actor, eventID, accountID string) error {
	if !validID(actor) || !validID(eventID) || !validID(accountID) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID string
	err = tx.QueryRowContext(ctx, `SELECT group_id FROM event_coordinators WHERE event_id=$1 AND account_id=$2 FOR UPDATE`, eventID, accountID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err = checkCoordinatorScope(ctx, tx, actor, groupID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM event_coordinators WHERE event_id=$1 AND account_id=$2`, eventID, accountID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_coordinator_audit_events(event_id,group_id,account_id,actor_id,action,occurred_at) VALUES($1,$2,$3,$4,'removed',$5)`, eventID, groupID, accountID, actor, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Coordinators(ctx context.Context, eventID string) ([]Coordinator, error) {
	if !validID(eventID) {
		return nil, ErrNotFound
	}
	return s.repo.coordinators(ctx, eventID, false)
}

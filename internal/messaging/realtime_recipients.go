package messaging

import "context"

func (r *Repository) directRealtimeRecipients(ctx context.Context, conversationID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT m.account_id::text
FROM direct_conversation_members m
JOIN accounts a ON a.id=m.account_id AND a.email_verified_at IS NOT NULL
WHERE m.conversation_id=$1 ORDER BY m.account_id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectAccountIDs(rows)
}

func (r *Repository) groupRealtimeRecipients(ctx context.Context, groupID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT a.id::text
FROM groups g
JOIN accounts a ON a.email_verified_at IS NOT NULL
WHERE g.id=$1 AND g.archived_at IS NULL AND (
 (g.audience='church' AND EXISTS(
  SELECT 1 FROM group_memberships gm WHERE gm.group_id=g.id AND gm.account_id=a.id AND gm.status='active'
 ))
 OR (g.audience='leadership' AND EXISTS(
  SELECT 1 FROM account_roles ar WHERE ar.account_id=a.id AND (
   (ar.role IN ('super_administrator','assistant_pastor') AND ar.scope='') OR ar.role='ministry_leader'
  )
 ))
) ORDER BY a.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectAccountIDs(rows)
}

func collectAccountIDs(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]string, error) {
	accounts := make([]string, 0)
	for rows.Next() {
		var accountID string
		if err := rows.Scan(&accountID); err != nil {
			return nil, err
		}
		accounts = append(accounts, accountID)
	}
	return accounts, rows.Err()
}

func (s *Service) DirectRealtimeRecipients(ctx context.Context, conversationID string) ([]string, error) {
	if !validID(conversationID) {
		return nil, ErrInvalid
	}
	return s.repo.directRealtimeRecipients(ctx, conversationID)
}

func (s *Service) GroupRealtimeRecipients(ctx context.Context, groupID string) ([]string, error) {
	if !validID(groupID) {
		return nil, ErrInvalid
	}
	return s.repo.groupRealtimeRecipients(ctx, groupID)
}

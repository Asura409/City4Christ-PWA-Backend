package access

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"
)

// MemberRecord contains only ordinary directory identity fields. Authentication,
// role, MFA, session, and privacy settings are deliberately excluded.
type MemberRecord struct {
	ID        string    `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Email     string    `json:"email"`
	JoinedAt  time.Time `json:"joinedAt"`
}

type MemberPage struct {
	Data       []MemberRecord `json:"data"`
	NextCursor string         `json:"nextCursor"`
}

func (s *Service) SearchMembers(ctx context.Context, actor, query, cursor string, limit int) (MemberPage, error) {
	page := MemberPage{Data: []MemberRecord{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "members.records.read", ""); err != nil {
		return page, err
	}
	query = strings.TrimSpace(query)
	if !utf8.ValidString(query) || strings.ContainsRune(query, 0) || utf8.RuneCountInString(query) < 3 || utf8.RuneCountInString(query) > 100 || limit < 1 || limit > 50 || (cursor != "" && !validID(cursor)) {
		return page, ErrInvalid
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	if cursor == "" {
		cursor = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.first_name,a.last_name,a.email,a.created_at FROM accounts a WHERE a.email_verified_at IS NOT NULL AND a.deletion_requested_at IS NULL AND a.id>$1::uuid AND (a.email ILIKE $2 ESCAPE '\' OR concat_ws(' ',a.first_name,a.last_name) ILIKE $2 ESCAPE '\') ORDER BY a.id LIMIT $3`, cursor, pattern, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MemberRecord
		if err = rows.Scan(&item.ID, &item.FirstName, &item.LastName, &item.Email, &item.JoinedAt); err != nil {
			return page, err
		}
		page.Data = append(page.Data, item)
	}
	if err = rows.Err(); err != nil && err != sql.ErrNoRows {
		return page, err
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[len(page.Data)-1].ID
	}
	return page, nil
}

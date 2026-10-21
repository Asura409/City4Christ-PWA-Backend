package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

type Candidate struct {
	ID         string `json:"id"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	Email      string `json:"email"`
	MFAEnabled bool   `json:"mfaEnabled"`
}
type CandidatePage struct {
	Data       []Candidate `json:"data"`
	NextCursor string      `json:"nextCursor"`
}

func (s *Service) ChurchList(ctx context.Context, actor, target string) ([]Assignment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "roles.read", ""); err != nil {
		return nil, err
	}
	if !validID(target) {
		return nil, ErrInvalid
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)`, target).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT role,scope FROM account_roles WHERE account_id=$1 ORDER BY role,scope`, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Assignment{}
	for rows.Next() {
		var a Assignment
		if err = rows.Scan(&a.Role, &a.Scope); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (s *Service) Candidates(ctx context.Context, actor, query, cursor string, limit int) (CandidatePage, error) {
	empty := CandidatePage{Data: []Candidate{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "roles.read", ""); err != nil {
		return empty, err
	}
	query = strings.TrimSpace(query)
	if !utf8.ValidString(query) || strings.ContainsRune(query, 0) || utf8.RuneCountInString(query) < 3 || utf8.RuneCountInString(query) > 100 || limit < 1 || limit > 50 || (cursor != "" && !validID(cursor)) {
		return empty, ErrInvalid
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query) + "%"
	if cursor == "" {
		cursor = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.first_name,a.last_name,a.email,EXISTS(SELECT 1 FROM mfa_factors m WHERE m.account_id=a.id AND m.confirmed_at IS NOT NULL) FROM accounts a WHERE a.email_verified_at IS NOT NULL AND a.id>$1::uuid AND (a.email ILIKE $2 ESCAPE '\' OR concat_ws(' ',a.first_name,a.last_name) ILIKE $2 ESCAPE '\') ORDER BY a.id LIMIT $3`, cursor, pattern, limit+1)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Candidate
		if err = rows.Scan(&c.ID, &c.FirstName, &c.LastName, &c.Email, &c.MFAEnabled); err != nil {
			return empty, err
		}
		empty.Data = append(empty.Data, c)
	}
	if err = rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	if len(empty.Data) > limit {
		empty.Data = empty.Data[:limit]
		empty.NextCursor = empty.Data[limit-1].ID
	}
	return empty, nil
}

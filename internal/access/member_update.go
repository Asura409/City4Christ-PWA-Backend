package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type MemberValidationError struct{ Fields map[string]string }

func (e *MemberValidationError) Error() string { return "invalid member name correction" }

type MemberNameUpdate struct {
	FirstName       *string `json:"firstName,omitempty"`
	LastName        *string `json:"lastName,omitempty"`
	ExpectedVersion int     `json:"expectedVersion"`
	Reason          string  `json:"reason"`
}

type MemberName struct {
	ID        string    `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	JoinedAt  time.Time `json:"joinedAt"`
	Version   int       `json:"version"`
}

func (s *Service) UpdateMemberName(ctx context.Context, actor, memberID string, in MemberNameUpdate) (MemberName, error) {
	var result MemberName
	if !validID(memberID) {
		return result, ErrMemberNotFound
	}
	fields := map[string]string{}
	if in.ExpectedVersion < 1 {
		fields["expectedVersion"] = "Supply the current member record version."
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsFunc(in.Reason, unicode.IsControl) {
		fields["reason"] = "Supply a reason of 1 to 500 characters without control characters. Do not include private care details."
	}
	for _, field := range []struct {
		name  string
		value **string
	}{{"firstName", &in.FirstName}, {"lastName", &in.LastName}} {
		if *field.value == nil {
			continue
		}
		value := strings.TrimSpace(**field.value)
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 100 || strings.ContainsFunc(value, unicode.IsControl) {
			fields[field.name] = "Use between 1 and 100 characters without control characters."
		}
		*field.value = &value
	}
	if in.FirstName == nil && in.LastName == nil {
		fields["body"] = "Supply firstName or lastName."
	}
	if len(fields) > 0 {
		return result, &MemberValidationError{Fields: fields}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = CheckTx(ctx, tx, actor, "members.records.manage", ""); err != nil {
		return result, err
	}
	var currentVersion int
	err = tx.QueryRowContext(ctx, `SELECT profile_version FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL AND deletion_requested_at IS NULL FOR UPDATE`, memberID).Scan(&currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrMemberNotFound
	}
	if err != nil {
		return result, err
	}
	if in.ExpectedVersion != currentVersion {
		return result, ErrVersionConflict
	}
	err = tx.QueryRowContext(ctx, `UPDATE accounts SET first_name=COALESCE($2,first_name),last_name=COALESCE($3,last_name),profile_version=profile_version+1 WHERE id=$1 RETURNING id,first_name,last_name,created_at,profile_version`, memberID, in.FirstName, in.LastName).
		Scan(&result.ID, &result.FirstName, &result.LastName, &result.JoinedAt, &result.Version)
	if err != nil {
		return MemberName{}, err
	}
	firstField, lastField := "", ""
	if in.FirstName != nil {
		firstField = "firstName"
	}
	if in.LastName != nil {
		lastField = "lastName"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO member_record_audit_events(actor_id,member_id,action,sensitive_fields,reason) VALUES($1,$2,'name_update',ARRAY(SELECT field FROM unnest(ARRAY[$3::text,$4::text]) AS field WHERE field<>''),$5)`, actor, memberID, firstField, lastField, in.Reason); err != nil {
		return MemberName{}, err
	}
	if err = tx.Commit(); err != nil {
		return MemberName{}, err
	}
	return result, nil
}

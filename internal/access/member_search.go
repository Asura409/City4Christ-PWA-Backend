package access

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/media"
)

// MemberRecord contains only ordinary directory identity fields. Authentication,
// role, MFA, session, and privacy settings are deliberately excluded.
type MemberRecord struct {
	ID        string            `json:"id"`
	FirstName string            `json:"firstName"`
	LastName  string            `json:"lastName"`
	Email     string            `json:"email"`
	JoinedAt  time.Time         `json:"joinedAt"`
	Version   int               `json:"version,omitempty"`
	Avatar    *media.Attachment `json:"avatar,omitempty"`
	avatar    *media.Attachment
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
	if !utf8.ValidString(query) || strings.ContainsRune(query, 0) || (query != "" && utf8.RuneCountInString(query) < 3) || utf8.RuneCountInString(query) > 100 || limit < 1 || limit > 50 || (cursor != "" && !validID(cursor)) {
		return page, ErrInvalid
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	if cursor == "" {
		cursor = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.first_name,a.last_name,a.email,a.created_at,a.profile_version,u.id::text,u.actual_content_type,u.actual_size,u.available_object_key FROM accounts a LEFT JOIN media_uploads u ON u.id=a.avatar_upload_id AND u.status='available' AND u.purpose='profile_photo' AND u.available_object_key IS NOT NULL WHERE a.email_verified_at IS NOT NULL AND a.deletion_requested_at IS NULL AND a.id>$1::uuid AND (a.email ILIKE $2 ESCAPE '\' OR concat_ws(' ',a.first_name,a.last_name) ILIKE $2 ESCAPE '\') ORDER BY a.id LIMIT $3 FOR SHARE OF a`, cursor, pattern, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MemberRecord
		var avatarID, contentType, objectKey sql.NullString
		var size sql.NullInt64
		if err = rows.Scan(&item.ID, &item.FirstName, &item.LastName, &item.Email, &item.JoinedAt, &item.Version, &avatarID, &contentType, &size, &objectKey); err != nil {
			return page, err
		}
		if avatarID.Valid && contentType.Valid && size.Valid && objectKey.Valid {
			item.avatar = &media.Attachment{ID: avatarID.String, ContentType: contentType.String, Size: size.Int64, ObjectKey: objectKey.String}
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
	if err = rows.Close(); err != nil {
		return MemberPage{}, err
	}
	for _, member := range page.Data {
		if _, err = tx.ExecContext(ctx, `INSERT INTO member_record_audit_events(actor_id,member_id,action,sensitive_fields)VALUES($1,$2,'search_result',ARRAY['email']::text[])`, actor, member.ID); err != nil {
			return MemberPage{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return MemberPage{}, err
	}
	if err = s.signMemberAvatars(ctx, page.Data); err != nil {
		return MemberPage{}, err
	}
	return page, nil
}

func (s *Service) signMemberAvatars(ctx context.Context, members []MemberRecord) error {
	if s.signer == nil {
		return nil
	}
	for i := range members {
		if members[i].avatar == nil {
			continue
		}
		signed, err := media.SignAttachments(ctx, s.signer, []media.Attachment{*members[i].avatar})
		if err != nil {
			return err
		}
		members[i].Avatar = &signed[0]
	}
	return nil
}

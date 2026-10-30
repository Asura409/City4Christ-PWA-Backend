package pastoral

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"time"
)

const profileColumns = `p.account_id,a.first_name,a.last_name,p.bio,p.time_zone,p.listed`
const eligiblePastor = `EXISTS(SELECT 1 FROM account_roles r WHERE r.account_id=p.account_id AND r.scope='' AND r.role IN ('assistant_pastor','super_administrator')) AND a.email_verified_at IS NOT NULL`

func lockPastor(ctx context.Context, tx *sql.Tx, id string, listed bool) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pastoral:'||$1,0))`, id); err != nil {
		return err
	}
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT p.listed FROM pastor_profiles p JOIN accounts a ON a.id=p.account_id WHERE p.account_id=$1 AND `+eligiblePastor+` FOR UPDATE OF p`, id).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if listed && !active {
		return ErrNotFound
	}
	// Keep verification and pastoral-role revocation serialized with this action.
	var role string
	err = tx.QueryRowContext(ctx, `SELECT r.role FROM account_roles r JOIN accounts a ON a.id=r.account_id WHERE r.account_id=$1 AND r.scope='' AND r.role IN ('assistant_pastor','super_administrator') AND a.email_verified_at IS NOT NULL ORDER BY r.role LIMIT 1 FOR SHARE OF r,a`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	return err
}
func audit(ctx context.Context, tx *sql.Tx, actor, pastor, kind, id, action string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO pastoral_audit_events(actor_id,pastor_id,resource_type,resource_id,action,occurred_at) VALUES($1,$2,$3,$4,$5,$6)`, actor, pastor, kind, id, action, now)
	return err
}
func (r *Repository) Appoint(ctx context.Context, actor string, in ProfileInput, guard Guard, now time.Time) (Profile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, err
	}
	defer tx.Rollback()
	if err = access.CheckTx(ctx, tx, actor, "roles.privileged.manage", ""); errors.Is(err, access.ErrForbidden) {
		return Profile{}, ErrForbidden
	}
	if err != nil {
		return Profile{}, err
	}
	if guard == nil {
		return Profile{}, ErrForbidden
	}
	if err = guard(ctx, tx); err != nil {
		return Profile{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pastoral:'||$1,0))`, in.AccountID); err != nil {
		return Profile{}, err
	}
	var target string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN account_roles r ON r.account_id=a.id WHERE a.id=$1 AND a.email_verified_at IS NOT NULL AND r.scope='' AND r.role IN ('assistant_pastor','super_administrator') ORDER BY r.role LIMIT 1 FOR SHARE OF a,r`, in.AccountID).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrInvalid
	}
	if err != nil {
		return Profile{}, err
	}
	listed := true
	if in.Listed != nil {
		listed = *in.Listed
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pastor_profiles(account_id,bio,time_zone,listed) VALUES($1,$2,$3,$4) ON CONFLICT(account_id) DO UPDATE SET bio=excluded.bio,time_zone=excluded.time_zone,listed=excluded.listed`, in.AccountID, in.Bio, in.TimeZone, listed)
	if err != nil {
		return Profile{}, err
	}
	if err = audit(ctx, tx, actor, in.AccountID, "profile", in.AccountID, "appointed", now); err != nil {
		return Profile{}, err
	}
	p, err := scanProfile(tx.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM pastor_profiles p JOIN accounts a ON a.id=p.account_id WHERE p.account_id=$1`, in.AccountID))
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func scanProfile(row interface{ Scan(...any) error }) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.FirstName, &p.LastName, &p.Bio, &p.TimeZone, &p.Listed)
	return p, err
}
func (r *Repository) Directory(ctx context.Context, o Options) ([]Profile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+profileColumns+` FROM pastor_profiles p JOIN accounts a ON a.id=p.account_id WHERE p.listed AND `+eligiblePastor+` AND ($1::uuid IS NULL OR p.account_id>$1) ORDER BY p.account_id LIMIT $2`, cursor(o.Cursor), o.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
func (r *Repository) OwnProfile(ctx context.Context, actor string) (Profile, error) {
	p, err := scanProfile(r.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM pastor_profiles p JOIN accounts a ON a.id=p.account_id WHERE p.account_id=$1 AND `+eligiblePastor, actor))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}
func cursor(raw string) any {
	if raw == "" {
		return nil
	}
	return raw
}

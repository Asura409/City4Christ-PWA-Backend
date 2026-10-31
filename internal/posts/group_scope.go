package posts

import (
	"context"
	"database/sql"
)

func visiblePostTx(ctx context.Context, tx *sql.Tx, actor, id string) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM church_posts p WHERE p.id=$1 AND p.deleted_at IS NULL AND c4c_group_content_visible(p.group_id,NULLIF($2,'')::uuid))`, id, actor).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
func (r *Repository) checkGroup(ctx context.Context, actor, group string) error {
	if group == "" {
		return nil
	}
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT c4c_group_content_visible($1::uuid,NULLIF($2,'')::uuid)`, group, actor).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

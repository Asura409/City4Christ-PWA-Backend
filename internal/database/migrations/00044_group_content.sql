-- +goose Up
-- Existing posts and events remain church-wide. Audiences cannot be moved by edits.
ALTER TABLE church_posts ADD COLUMN group_id uuid REFERENCES groups(id);
ALTER TABLE church_events ADD COLUMN group_id uuid REFERENCES groups(id);
CREATE INDEX church_posts_group_feed ON church_posts(group_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX church_events_group_start ON church_events(group_id,starts_at,id);
-- Shared read predicate: membership never bypasses leadership eligibility.
-- +goose StatementBegin
CREATE FUNCTION c4c_group_content_visible(group_id uuid, viewer uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT $1 IS NULL OR EXISTS(
 SELECT 1 FROM groups g JOIN group_memberships m ON m.group_id=g.id
 JOIN accounts a ON a.id=m.account_id
 WHERE g.id=$1 AND m.account_id=$2 AND m.status='active'
 AND g.archived_at IS NULL AND a.email_verified_at IS NOT NULL
 AND (g.audience='church' OR EXISTS(SELECT 1 FROM account_roles r WHERE r.account_id=$2
 AND ((r.role IN ('super_administrator','assistant_pastor') AND r.scope='') OR r.role='ministry_leader'))))
$$;
-- +goose StatementEnd

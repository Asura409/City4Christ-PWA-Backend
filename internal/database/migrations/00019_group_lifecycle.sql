-- +goose Up
ALTER TABLE group_audit_events DROP CONSTRAINT group_audit_events_action_check;
ALTER TABLE group_audit_events ADD CONSTRAINT group_audit_events_action_check
 CHECK (action IN ('created','requested','joined','approved','declined','left','archived','restored'));

-- +goose Down
DELETE FROM group_audit_events WHERE action IN ('created','archived','restored');
ALTER TABLE group_audit_events DROP CONSTRAINT group_audit_events_action_check;
ALTER TABLE group_audit_events ADD CONSTRAINT group_audit_events_action_check
 CHECK (action IN ('requested','joined','approved','declined','left'));

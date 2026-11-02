-- +goose Up
ALTER TABLE group_audit_events DROP CONSTRAINT group_audit_events_action_check;
ALTER TABLE group_audit_events ADD CONSTRAINT group_audit_events_action_check
 CHECK (action IN ('created','requested','joined','approved','declined','left','archived','restored','removed'));

-- +goose Down
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM group_audit_events WHERE action='removed') THEN
    RAISE EXCEPTION 'cannot remove group member removal audit support while removal events exist';
  END IF;
END $$;
ALTER TABLE group_audit_events DROP CONSTRAINT group_audit_events_action_check;
ALTER TABLE group_audit_events ADD CONSTRAINT group_audit_events_action_check
 CHECK (action IN ('created','requested','joined','approved','declined','left','archived','restored'));

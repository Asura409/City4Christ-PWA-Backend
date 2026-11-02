-- +goose Up
ALTER TABLE notification_broadcasts
  ADD COLUMN staff_only boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT notification_broadcasts_staff_audience_check
    CHECK (NOT staff_only OR group_id IS NULL);

-- +goose Down
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM notification_broadcasts WHERE staff_only) THEN
    RAISE EXCEPTION 'cannot remove staff broadcast support while staff-only broadcasts exist';
  END IF;
END $$;
ALTER TABLE notification_broadcasts
  DROP CONSTRAINT notification_broadcasts_staff_audience_check,
  DROP COLUMN staff_only;

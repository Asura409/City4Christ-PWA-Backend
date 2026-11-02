-- +goose Up
ALTER TABLE media_uploads DROP CONSTRAINT media_uploads_purpose_check;
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_purpose_check CHECK (
    purpose IN ('post_attachment','sermon_attachment','message_attachment','profile_photo')
);

ALTER TABLE accounts ADD COLUMN avatar_upload_id uuid REFERENCES media_uploads(id) ON DELETE SET NULL;
CREATE INDEX accounts_avatar_upload_idx ON accounts(avatar_upload_id) WHERE avatar_upload_id IS NOT NULL;

-- +goose Down
DROP INDEX accounts_avatar_upload_idx;
ALTER TABLE accounts DROP COLUMN avatar_upload_id;
ALTER TABLE media_uploads DROP CONSTRAINT media_uploads_purpose_check;
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_purpose_check
    CHECK (purpose IN ('post_attachment','sermon_attachment','message_attachment'));

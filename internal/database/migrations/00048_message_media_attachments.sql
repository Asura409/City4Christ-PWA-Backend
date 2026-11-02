-- +goose Up
ALTER TABLE media_uploads DROP CONSTRAINT media_uploads_purpose_check;
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_purpose_check
    CHECK (purpose IN ('post_attachment','sermon_attachment','message_attachment'));

CREATE TABLE direct_message_media_attachments (
    message_id uuid NOT NULL REFERENCES direct_messages(id) ON DELETE CASCADE,
    upload_id uuid NOT NULL REFERENCES media_uploads(id) ON DELETE CASCADE,
    position smallint NOT NULL CHECK (position >= 0),
    PRIMARY KEY (message_id, position),
    UNIQUE (message_id, upload_id)
);
CREATE INDEX direct_message_media_upload_idx ON direct_message_media_attachments(upload_id);

CREATE TABLE group_chat_message_media_attachments (
    message_id uuid NOT NULL REFERENCES group_chat_messages(id) ON DELETE CASCADE,
    upload_id uuid NOT NULL REFERENCES media_uploads(id) ON DELETE CASCADE,
    position smallint NOT NULL CHECK (position >= 0),
    PRIMARY KEY (message_id, position),
    UNIQUE (message_id, upload_id)
);
CREATE INDEX group_chat_message_media_upload_idx ON group_chat_message_media_attachments(upload_id);

-- +goose Down
DROP TABLE group_chat_message_media_attachments;
DROP TABLE direct_message_media_attachments;
ALTER TABLE media_uploads DROP CONSTRAINT media_uploads_purpose_check;
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_purpose_check
    CHECK (purpose IN ('post_attachment','sermon_attachment'));

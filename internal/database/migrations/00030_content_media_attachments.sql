-- +goose Up
CREATE TABLE post_media_attachments (
    post_id uuid NOT NULL REFERENCES church_posts(id) ON DELETE CASCADE,
    upload_id uuid NOT NULL REFERENCES media_uploads(id) ON DELETE CASCADE,
    position smallint NOT NULL CHECK (position >= 0),
    PRIMARY KEY (post_id, position)
);

CREATE TABLE sermon_media_attachments (
    sermon_id uuid NOT NULL REFERENCES sermons(id) ON DELETE CASCADE,
    upload_id uuid NOT NULL REFERENCES media_uploads(id) ON DELETE CASCADE,
    position smallint NOT NULL CHECK (position >= 0),
    PRIMARY KEY (sermon_id, position)
);

CREATE INDEX post_media_attachments_upload_idx ON post_media_attachments(upload_id);
CREATE INDEX sermon_media_attachments_upload_idx ON sermon_media_attachments(upload_id);

-- +goose Down
DROP TABLE sermon_media_attachments;
DROP TABLE post_media_attachments;

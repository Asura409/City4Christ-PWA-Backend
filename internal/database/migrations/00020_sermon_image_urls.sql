-- +goose Up
ALTER TABLE sermons ADD COLUMN image_url text NOT NULL DEFAULT '';
ALTER TABLE sermons ADD CONSTRAINT sermons_image_url_safe CHECK (
    image_url = '' OR image_url ~ '^https://images[.]unsplash[.]com/photo-[A-Za-z0-9_-]+([?][A-Za-z0-9_.=&%+-]*)?$'
);

-- +goose Down
ALTER TABLE sermons DROP CONSTRAINT sermons_image_url_safe;
ALTER TABLE sermons DROP COLUMN image_url;

-- +goose Up
CREATE TABLE sermons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    summary text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 10000),
    speaker text NOT NULL CHECK (char_length(btrim(speaker)) BETWEEN 1 AND 200),
    series text NOT NULL DEFAULT '' CHECK (char_length(series) <= 200),
    topic text NOT NULL DEFAULT '' CHECK (char_length(topic) <= 100),
    preached_at timestamptz NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- NULL publication is a draft; future publication timestamps remain hidden.
CREATE INDEX sermons_published_order ON sermons(published_at DESC,id DESC)
    WHERE published_at IS NOT NULL;
CREATE INDEX sermons_speaker ON sermons(lower(speaker),published_at DESC,id DESC)
    WHERE published_at IS NOT NULL;
CREATE INDEX sermons_series ON sermons(lower(series),published_at DESC,id DESC)
    WHERE published_at IS NOT NULL;
CREATE INDEX sermons_topic ON sermons(lower(topic),published_at DESC,id DESC)
    WHERE published_at IS NOT NULL;

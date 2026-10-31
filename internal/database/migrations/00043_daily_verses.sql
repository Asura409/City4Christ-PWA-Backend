-- +goose Up
CREATE TABLE daily_verses (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 display_date date NOT NULL CHECK(display_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 text text NOT NULL CHECK(char_length(text) BETWEEN 1 AND 4000),
 scripture_reference text NOT NULL CHECK(char_length(scripture_reference) BETWEEN 1 AND 200),
 translation text NOT NULL DEFAULT '' CHECK(char_length(translation)<=100),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published')),
 published_at timestamptz,
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK((status='draft' AND published_at IS NULL) OR (status='published' AND published_at IS NOT NULL))
);
CREATE UNIQUE INDEX daily_verses_published_date ON daily_verses(display_date) WHERE status='published';
CREATE INDEX daily_verses_editorial_page ON daily_verses(created_at DESC,id DESC);
CREATE TABLE daily_verse_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 verse_id uuid NOT NULL REFERENCES daily_verses(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('created','updated','published','withdrawn')),
 version integer NOT NULL,
 occurred_at timestamptz NOT NULL
);

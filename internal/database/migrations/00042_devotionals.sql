-- +goose Up
CREATE TABLE devotionals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 title text NOT NULL CHECK(char_length(title) BETWEEN 1 AND 200),
 body text NOT NULL CHECK(char_length(body) BETWEEN 1 AND 20000),
 scripture_reference text NOT NULL DEFAULT '' CHECK(char_length(scripture_reference)<=200),
 author text NOT NULL DEFAULT '' CHECK(char_length(author)<=200),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published')),
 published_at timestamptz,
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK((status='draft' AND published_at IS NULL) OR (status='published' AND published_at IS NOT NULL))
);
CREATE INDEX devotionals_editorial_page ON devotionals(created_at DESC,id DESC);
CREATE INDEX devotionals_published_page ON devotionals(published_at DESC,id DESC) WHERE status='published';
CREATE TABLE devotional_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 devotional_id uuid NOT NULL REFERENCES devotionals(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('created','updated','published','withdrawn')),
 version integer NOT NULL,
 occurred_at timestamptz NOT NULL
);

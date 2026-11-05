-- +goose Up
CREATE TABLE bible_translations (
  id text PRIMARY KEY,
  name text NOT NULL,
  language text NOT NULL DEFAULT 'English',
  license text NOT NULL,
  source_url text NOT NULL,
  can_import boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE bible_verses (
  translation_id text NOT NULL REFERENCES bible_translations(id) ON DELETE CASCADE,
  book_id text NOT NULL,
  book_name text NOT NULL,
  chapter integer NOT NULL CHECK (chapter > 0),
  verse integer NOT NULL CHECK (verse > 0),
  text text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 30000),
  PRIMARY KEY (translation_id, book_id, chapter, verse)
);

CREATE INDEX bible_verses_lookup_idx
  ON bible_verses (translation_id, book_id, chapter, verse);

CREATE TABLE bible_import_jobs (
  translation_id text PRIMARY KEY REFERENCES bible_translations(id) ON DELETE CASCADE,
  status text NOT NULL CHECK (status IN ('pending', 'running', 'failed', 'complete')),
  last_book_id text,
  last_chapter integer CHECK (last_chapter IS NULL OR last_chapter > 0),
  imported_verses integer NOT NULL DEFAULT 0 CHECK (imported_verses >= 0),
  error text,
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO bible_translations (id, name, language, license, source_url, can_import)
VALUES ('kjv', 'King James Version', 'English', 'Public domain via HelloAO', 'https://bible.helloao.org/', true);

INSERT INTO bible_import_jobs (translation_id, status)
VALUES ('kjv', 'pending');

-- +goose Down
DROP TABLE bible_import_jobs;
DROP TABLE bible_verses;
DROP TABLE bible_translations;

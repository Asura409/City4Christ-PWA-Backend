package bible

import (
	"context"
	"database/sql"
	"strings"
)

// Database serves imported public-domain translations from PostgreSQL. An
// empty catalog intentionally reports no translations so the remote provider
// can serve KJV until the one-time import has completed.
type Database struct{ db *sql.DB }

func NewDatabase(db *sql.DB) *Database { return &Database{db: db} }

func (d *Database) Translations(ctx context.Context) ([]Translation, error) {
	if d == nil || d.db == nil {
		return nil, ErrUnavailable
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.language, t.license, t.can_import
		FROM bible_translations t
		WHERE EXISTS (SELECT 1 FROM bible_verses v WHERE v.translation_id = t.id)
		ORDER BY t.name`)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	result := []Translation{}
	for rows.Next() {
		var t Translation
		if err := rows.Scan(&t.ID, &t.Name, &t.Language, &t.License, &t.CanImport); err != nil {
			return nil, ErrUnavailable
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (d *Database) Lookup(ctx context.Context, q Query) (Passage, error) {
	if d == nil || d.db == nil || strings.ToLower(strings.TrimSpace(q.Translation)) != "kjv" {
		return Passage{}, invalid("translation", "Choose a supported translation.")
	}
	id, reference, err := passageID(q.Reference)
	if err != nil {
		return Passage{}, err
	}
	parts := strings.Split(id, ".")
	if len(parts) != 3 {
		return Passage{}, invalid("reference", "Choose a verse or verse range for database lookup.")
	}
	rangeParts := strings.Split(parts[2], "-")
	start, end := atoi(rangeParts[0]), atoi(rangeParts[0])
	if len(rangeParts) == 2 {
		end = atoi(rangeParts[1])
	}
	if start < 1 || end < start {
		return Passage{}, invalid("reference", "Use a valid verse or verse range.")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT book_id, book_name, chapter, verse, text FROM bible_verses WHERE translation_id=$1 AND book_id=$2 AND chapter=$3 AND verse BETWEEN $4 AND $5 ORDER BY verse`, "kjv", parts[0], atoi(parts[1]), start, end)
	if err != nil {
		return Passage{}, ErrUnavailable
	}
	defer rows.Close()
	verses := []Verse{}
	for rows.Next() {
		var verse Verse
		if err := rows.Scan(&verse.BookID, &verse.BookName, &verse.Chapter, &verse.Number, &verse.Text); err != nil {
			return Passage{}, ErrUnavailable
		}
		verses = append(verses, verse)
	}
	if err := rows.Err(); err != nil || len(verses) == 0 {
		return Passage{}, ErrUnavailable
	}
	texts := make([]string, 0, len(verses))
	for _, verse := range verses {
		texts = append(texts, "["+itoa(verse.Number)+"] "+verse.Text)
	}
	return Passage{Reference: reference, Translation: Translation{ID: "kjv", Name: "King James Version", Language: "English", License: "Public domain via HelloAO", CanImport: true}, Text: strings.Join(texts, "\n"), Verses: verses, Source: "City4Christ database", SourceURL: "https://bible.helloao.org/"}, nil
}

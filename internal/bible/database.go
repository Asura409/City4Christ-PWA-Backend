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

type databaseReference struct {
	bookID, bookName         string
	startChapter, endChapter int
	startVerse, endVerse     int
	hasVerse                 bool
}

func parseDatabaseReference(reference string) (databaseReference, string, error) {
	m := referencePattern.FindStringSubmatch(strings.TrimSpace(reference))
	if m == nil {
		return databaseReference{}, "", invalid("reference", "Use a book, chapter and optional verse range.")
	}
	bookID, ok := bookIDs[strings.ToLower(strings.TrimSpace(m[1]))]
	if !ok {
		return databaseReference{}, "", invalid("reference", "Use a recognized English Bible book name.")
	}
	startChapter := atoi(m[2])
	result := databaseReference{
		bookID:       bookID,
		bookName:     bookNames[bookID],
		startChapter: startChapter,
		endChapter:   startChapter,
	}
	if startChapter < 1 {
		return databaseReference{}, "", invalid("reference", "Use a valid chapter number.")
	}
	if m[3] == "" {
		return result, bookNames[bookID] + " " + m[2], nil
	}
	result.hasVerse = true
	result.startVerse = atoi(m[3])
	result.endVerse = result.startVerse
	if m[5] != "" {
		result.endVerse = atoi(m[5])
		if m[4] != "" {
			result.endChapter = atoi(m[4])
		}
	}
	if result.startChapter < 1 || result.endChapter < result.startChapter || result.startVerse < 1 || result.endVerse < 1 || (result.endChapter == result.startChapter && result.endVerse < result.startVerse) {
		return databaseReference{}, "", invalid("reference", "Use a valid verse or verse range.")
	}
	displayReference := bookNames[bookID] + " " + m[2] + ":" + m[3]
	if m[5] != "" {
		displayReference += "-"
		if m[4] != "" {
			displayReference += m[4] + ":"
		}
		displayReference += m[5]
	}
	return result, displayReference, nil
}

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
	parsed, reference, err := parseDatabaseReference(q.Reference)
	if err != nil {
		return Passage{}, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT book_id, book_name, chapter, verse, text FROM bible_verses WHERE translation_id=$1 AND book_id=$2 AND chapter BETWEEN $3 AND $4 ORDER BY chapter, verse`, "kjv", parsed.bookID, parsed.startChapter, parsed.endChapter)
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
		if parsed.hasVerse {
			if verse.Chapter == parsed.startChapter && verse.Number < parsed.startVerse {
				continue
			}
			if verse.Chapter == parsed.endChapter && verse.Number > parsed.endVerse {
				continue
			}
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

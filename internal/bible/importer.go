package bible

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type KJVImporter struct {
	db     *sql.DB
	client *http.Client
}

func NewKJVImporter(db *sql.DB, client *http.Client) *KJVImporter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &KJVImporter{db: db, client: client}
}

// ImportKJV imports HelloAO's public-domain KJV in resumable, idempotent
// chapter-sized transactions. It is safe to retry after a worker restart.
func (i *KJVImporter) ImportKJV(ctx context.Context) error {
	if i == nil || i.db == nil || i.client == nil {
		return errors.New("KJV importer is not configured")
	}
	var locked bool
	if err := i.db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext('city4christ:bible:kjv-import'))`).Scan(&locked); err != nil {
		return fmt.Errorf("acquire KJV import lock: %w", err)
	}
	if !locked {
		return nil
	}
	defer i.db.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('city4christ:bible:kjv-import'))`)

	var status string
	var lastBook sql.NullString
	var lastChapter sql.NullInt64
	err := i.db.QueryRowContext(ctx, `SELECT status, last_book_id, last_chapter FROM bible_import_jobs WHERE translation_id='kjv'`).Scan(&status, &lastBook, &lastChapter)
	if err != nil {
		return fmt.Errorf("read KJV import job: %w", err)
	}
	if status == "complete" {
		return nil
	}
	if _, err := i.db.ExecContext(ctx, `UPDATE bible_import_jobs SET status='running', error=NULL, updated_at=now() WHERE translation_id='kjv'`); err != nil {
		return fmt.Errorf("start KJV import: %w", err)
	}

	books, err := i.books(ctx)
	if err != nil {
		i.fail(ctx, err)
		return err
	}
	sort.Slice(books, func(a, b int) bool { return books[a].Order < books[b].Order })
	resume := lastBook.String
	resumeChapter := int(lastChapter.Int64)
	resuming := resume != ""
	for _, book := range books {
		if resuming && book.ID != resume {
			continue
		}
		firstChapter := 1
		if resuming && book.ID == resume {
			firstChapter = resumeChapter + 1
			resuming = false
		}
		for chapter := firstChapter; chapter <= book.NumberOfChapters; chapter++ {
			if err := i.importChapter(ctx, book, chapter); err != nil {
				i.fail(ctx, err)
				return err
			}
			if _, err := i.db.ExecContext(ctx, `UPDATE bible_import_jobs SET status='running', last_book_id=$1, last_chapter=$2, imported_verses=(SELECT count(*) FROM bible_verses WHERE translation_id='kjv'), updated_at=now() WHERE translation_id='kjv'`, book.ID, chapter); err != nil {
				return fmt.Errorf("save KJV import progress: %w", err)
			}
		}
	}
	if resuming {
		return fmt.Errorf("KJV import checkpoint book %q was not found", resume)
	}
	_, err = i.db.ExecContext(ctx, `UPDATE bible_import_jobs SET status='complete', error=NULL, imported_verses=(SELECT count(*) FROM bible_verses WHERE translation_id='kjv'), updated_at=now() WHERE translation_id='kjv'`)
	return err
}

type helloAOBooksResponse struct {
	Books []helloAOBook `json:"books"`
}

type helloAOBook struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Order            int    `json:"order"`
	NumberOfChapters int    `json:"numberOfChapters"`
}

func (i *KJVImporter) books(ctx context.Context) ([]helloAOBook, error) {
	var response helloAOBooksResponse
	if err := i.getJSON(ctx, helloAOBaseURL+"/eng_kjv/books.json", &response); err != nil {
		return nil, err
	}
	if len(response.Books) == 0 {
		return nil, errors.New("HelloAO returned no KJV books")
	}
	return response.Books, nil
}

func (i *KJVImporter) importChapter(ctx context.Context, book helloAOBook, chapter int) error {
	var response helloAOChapter
	if err := i.getJSON(ctx, fmt.Sprintf("%s/eng_kjv/%s/%d.json", helloAOBaseURL, book.ID, chapter), &response); err != nil {
		return err
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range response.Chapter.Content {
		if item.Type != "verse" {
			continue
		}
		text := strings.TrimSpace(flattenHelloAOContent(item.Content))
		if item.Number < 1 || text == "" || !plain(text, 30000) {
			return fmt.Errorf("invalid KJV verse %s %d:%d", book.ID, chapter, item.Number)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bible_verses (translation_id, book_id, book_name, chapter, verse, text) VALUES ('kjv',$1,$2,$3,$4,$5) ON CONFLICT (translation_id, book_id, chapter, verse) DO UPDATE SET book_name=EXCLUDED.book_name, text=EXCLUDED.text`, book.ID, book.Name, chapter, item.Number, text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *KJVImporter) fail(ctx context.Context, cause error) {
	_, _ = i.db.ExecContext(ctx, `UPDATE bible_import_jobs SET status='failed', error=$1, updated_at=now() WHERE translation_id='kjv'`, cause.Error())
}

func (i *KJVImporter) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HelloAO returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil || len(body) == 0 || !json.Valid(body) {
		return errors.New("invalid HelloAO JSON response")
	}
	return json.Unmarshal(body, target)
}

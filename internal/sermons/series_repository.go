package sermons

import (
	"context"
	"time"
)

type SeriesSummary struct {
	Name              string    `json:"name"`
	SermonCount       int       `json:"sermonCount"`
	LatestPublishedAt time.Time `json:"latestPublishedAt"`
}

func (r *Repository) ListSeries(ctx context.Context, now time.Time) ([]SeriesSummary, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT MIN(BTRIM(series)) AS name, COUNT(*) AS sermon_count, MAX(published_at) AS latest_published_at
		FROM sermons
		WHERE published_at <= $1 AND BTRIM(series) <> ''
		GROUP BY LOWER(BTRIM(series))
		ORDER BY MAX(published_at) DESC, LOWER(MIN(BTRIM(series))) ASC`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SeriesSummary, 0)
	for rows.Next() {
		var item SeriesSummary
		if err := rows.Scan(&item.Name, &item.SermonCount, &item.LatestPublishedAt); err != nil {
			return nil, err
		}
		item.LatestPublishedAt = item.LatestPublishedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

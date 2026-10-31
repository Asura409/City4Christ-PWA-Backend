package devotionals

import (
	"errors"
	"time"
)

var ErrVerseDateConflict = errors.New("a verse is already published for this church date")

// The owner chose fixed GMT+1, with no daylight-saving changes.
var churchTime = time.FixedZone("UTC+01:00", 60*60)

type VerseContent struct {
	DisplayDate        string `json:"displayDate"`
	Text               string `json:"text"`
	ScriptureReference string `json:"scriptureReference"`
	Translation        string `json:"translation"`
}
type PublishedVerse struct {
	ID string `json:"id"`
	VerseContent
}
type Verse struct {
	PublishedVerse
	Status      string     `json:"status"`
	Version     int        `json:"version"`
	PublishedAt *time.Time `json:"publishedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}
type VerseUpdate struct {
	Version            int     `json:"version"`
	DisplayDate        *string `json:"displayDate"`
	Text               *string `json:"text"`
	ScriptureReference *string `json:"scriptureReference"`
	Translation        *string `json:"translation"`
	Status             *string `json:"status"`
}
type TodayVerse struct {
	Date       string          `json:"date"`
	TimeZone   string          `json:"timeZone"`
	ServerTime time.Time       `json:"serverTime"`
	NextDayAt  time.Time       `json:"nextDayAt"`
	Verse      *PublishedVerse `json:"verse"`
}

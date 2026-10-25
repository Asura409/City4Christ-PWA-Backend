// Package prayer implements privacy-scoped member prayer requests.
package prayer

import "time"

type Request struct {
	ID          string     `json:"id"`
	Body        string     `json:"body"`
	Visibility  string     `json:"visibility"`
	GroupID     string     `json:"groupId,omitempty"`
	Anonymous   bool       `json:"anonymous"`
	Status      string     `json:"status"`
	Author      string     `json:"author"`
	IsOwner     bool       `json:"isOwner"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	AnsweredAt  *time.Time `json:"answeredAt,omitempty"`
	PrayerCount int        `json:"prayerCount"`
	HasPrayed   bool       `json:"hasPrayed"`
}

type CreateInput struct {
	Body       string `json:"body"`
	Visibility string `json:"visibility"`
	GroupID    string `json:"groupId,omitempty"`
	Anonymous  bool   `json:"anonymous"`
}

type Options struct {
	Limit  int
	Cursor string
	Status string
}

type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type Page struct {
	Items      []Request `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type Acknowledgement struct {
	Changed bool `json:"changed"`
}

type StatusResult struct {
	Changed bool `json:"changed"`
}

// Report deliberately omits reporter and author identity. Moderators receive
// only the request content they are authorized to review.
type Report struct {
	ID                string    `json:"id"`
	RequestID         string    `json:"requestId"`
	RequestBody       string    `json:"requestBody"`
	RequestVisibility string    `json:"requestVisibility"`
	GroupID           string    `json:"groupId,omitempty"`
	Anonymous         bool      `json:"anonymous"`
	Reason            string    `json:"reason"`
	Details           string    `json:"details,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
}

type ReportInput struct {
	Reason  string `json:"reason"`
	Details string `json:"details,omitempty"`
}

type ReviewInput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type ReportPage struct {
	Items []Report `json:"items"`
}

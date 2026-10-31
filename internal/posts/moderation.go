package posts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

var (
	ErrReportInvalid       = errors.New("invalid content report")
	ErrReportNotFound      = errors.New("content report not found")
	ErrReportConflict      = errors.New("content report state conflict")
	ErrModerationForbidden = errors.New("content moderation forbidden")
)

type ReportInput struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Reason     string `json:"reason"`
	Details    string `json:"details,omitempty"`
}

type Report struct {
	ID         string    `json:"id"`
	TargetType string    `json:"targetType"`
	TargetID   string    `json:"targetId"`
	PostID     string    `json:"postId"`
	Content    string    `json:"content"`
	Reason     string    `json:"reason"`
	Details    string    `json:"details"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ReportOptions struct {
	GroupID string
	Status  string
	Limit   int
	Cursor  string
}

type ReportPage struct {
	Items      []Report `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type ReviewInput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type reportCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type ModerationRepository struct{ db *sql.DB }

func NewModerationRepository(db *sql.DB) *ModerationRepository {
	return &ModerationRepository{db: db}
}

type ModerationService struct {
	repo        *ModerationRepository
	permissions *access.Service
	now         func() time.Time
}

func NewModerationService(repo *ModerationRepository, permissions *access.Service, clocks ...func() time.Time) *ModerationService {
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &ModerationService{repo: repo, permissions: permissions, now: now}
}

func (s *ModerationService) Report(ctx context.Context, actor string, input ReportInput) (Report, error) {
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Details = strings.TrimSpace(input.Details)
	if !validID(actor) || !validID(input.TargetID) || !validReportReason(input.Reason) ||
		!utf8.ValidString(input.Details) || len([]rune(input.Details)) > 1000 || strings.ContainsRune(input.Details, 0) ||
		(input.TargetType != "post" && input.TargetType != "comment") {
		return Report{}, ErrReportInvalid
	}
	return s.repo.Report(ctx, actor, input, s.now().UTC())
}

func validReportReason(reason string) bool {
	switch reason {
	case "spam", "harassment", "inappropriate", "hate", "threats", "other":
		return true
	default:
		return false
	}
}

func (s *ModerationService) ListReports(ctx context.Context, actor string, options ReportOptions) (ReportPage, error) {
	if !validID(actor) {
		return ReportPage{}, ErrModerationForbidden
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Status == "" {
		options.Status = "pending"
	}
	if (options.GroupID != "" && !validID(options.GroupID)) || options.Limit < 1 || options.Limit > 100 || (options.Status != "pending" && options.Status != "resolved" && options.Status != "dismissed") || len(options.Cursor) > 2048 {
		return ReportPage{}, ErrReportInvalid
	}
	var after *reportCursor
	if options.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil {
			return ReportPage{}, ErrReportInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var cursor *reportCursor
		if decoder.Decode(&cursor) != nil || cursor == nil || decoder.Decode(new(any)) != io.EOF || cursor.CreatedAt.IsZero() || !validID(cursor.ID) {
			return ReportPage{}, ErrReportInvalid
		}
		after = cursor
	}
	if s.permissions == nil {
		return ReportPage{}, ErrModerationForbidden
	}
	items, err := s.repo.List(ctx, actor, options, after)
	if err != nil {
		return ReportPage{}, err
	}
	page := ReportPage{Items: items}
	if len(items) > options.Limit {
		page.Items = items[:options.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(reportCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return ReportPage{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (s *ModerationService) Review(ctx context.Context, actor, reportID string, input ReviewInput) error {
	input.Decision = strings.TrimSpace(input.Decision)
	input.Reason = strings.TrimSpace(input.Reason)
	if !validID(actor) || !validID(reportID) || (input.Decision != "hide" && input.Decision != "dismiss") ||
		!utf8.ValidString(input.Reason) || len([]rune(input.Reason)) < 1 || len([]rune(input.Reason)) > 500 || strings.ContainsRune(input.Reason, 0) {
		return ErrReportInvalid
	}
	if s.permissions == nil {
		return ErrModerationForbidden
	}
	return s.repo.Review(ctx, s.permissions, actor, reportID, input, s.now().UTC())
}

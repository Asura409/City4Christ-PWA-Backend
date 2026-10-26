package prayer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrForbidden = errors.New("prayer request action forbidden")
	ErrInvalid   = errors.New("invalid prayer request")
	ErrConflict  = errors.New("prayer request state conflict")
)

const requestLifetime = 180 * 24 * time.Hour

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository, clocks ...func() time.Time) *Service {
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &Service{repo: repo, now: now}
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id != uuid.Nil
}

func (s *Service) Create(ctx context.Context, actor string, input CreateInput) (Request, error) {
	input.Body = strings.TrimSpace(input.Body)
	if !validID(actor) || !utf8.ValidString(input.Body) || len([]rune(input.Body)) < 1 || len([]rune(input.Body)) > 3000 || strings.ContainsRune(input.Body, 0) {
		return Request{}, ErrInvalid
	}
	switch input.Visibility {
	case "community", "private":
		if input.GroupID != "" {
			return Request{}, ErrInvalid
		}
	case "group":
		if !validID(input.GroupID) {
			return Request{}, ErrInvalid
		}
		active, err := s.repo.IsActiveGroupMember(ctx, actor, input.GroupID)
		if err != nil {
			return Request{}, err
		}
		if !active {
			return Request{}, ErrForbidden
		}
	default:
		return Request{}, ErrInvalid
	}
	return s.repo.Create(ctx, actor, input, s.now().UTC())
}

func (s *Service) List(ctx context.Context, actor string, options Options) (Page, error) {
	if !validID(actor) {
		return Page{}, ErrForbidden
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 || len(options.Cursor) > 2048 {
		return Page{}, ErrInvalid
	}
	if options.Status == "" {
		options.Status = "open"
	}
	if options.Status != "open" && options.Status != "answered" {
		return Page{}, ErrInvalid
	}
	var after *Cursor
	if options.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil {
			return Page{}, ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var cursor *Cursor
		if decoder.Decode(&cursor) != nil || cursor == nil || decoder.Decode(new(any)) != io.EOF || cursor.CreatedAt.IsZero() || !validID(cursor.ID) {
			return Page{}, ErrInvalid
		}
		after = cursor
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return Page{}, err
	}
	items, err := s.repo.List(ctx, actor, options, after)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > options.Limit {
		page.Items = items[:options.Limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, actor, requestID string) (Request, error) {
	if !validID(actor) {
		return Request{}, ErrForbidden
	}
	if !validID(requestID) {
		return Request{}, ErrNotFound
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return Request{}, err
	}
	return s.repo.Get(ctx, actor, requestID)
}

func (s *Service) SetStatus(ctx context.Context, actor, requestID, status string) (bool, error) {
	if !validID(actor) {
		return false, ErrForbidden
	}
	if !validID(requestID) {
		return false, ErrNotFound
	}
	if status != "answered" && status != "archived" {
		return false, ErrInvalid
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return false, err
	}
	return s.repo.SetStatus(ctx, actor, requestID, status)
}

func (s *Service) Pray(ctx context.Context, actor, requestID string) (Acknowledgement, error) {
	if !validID(actor) {
		return Acknowledgement{}, ErrForbidden
	}
	if !validID(requestID) {
		return Acknowledgement{}, ErrNotFound
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return Acknowledgement{}, err
	}
	changed, err := s.repo.Pray(ctx, actor, requestID)
	return Acknowledgement{Changed: changed}, err
}

func (s *Service) ListEncouragements(ctx context.Context, actor, requestID string) ([]Encouragement, error) {
	if !validID(actor) {
		return nil, ErrForbidden
	}
	if !validID(requestID) {
		return nil, ErrNotFound
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return nil, err
	}
	return s.repo.ListEncouragements(ctx, actor, requestID)
}

func (s *Service) CreateEncouragement(ctx context.Context, actor, requestID string, input EncouragementInput) (Encouragement, error) {
	input.Body = strings.TrimSpace(input.Body)
	if !validID(actor) || !validID(requestID) || !utf8.ValidString(input.Body) || len([]rune(input.Body)) < 1 || len([]rune(input.Body)) > 1000 || strings.ContainsRune(input.Body, 0) {
		return Encouragement{}, ErrInvalid
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return Encouragement{}, err
	}
	return s.repo.CreateEncouragement(ctx, actor, requestID, input.Body, s.now().UTC())
}

func (s *Service) Report(ctx context.Context, actor, requestID string, input ReportInput) (Report, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	input.Details = strings.TrimSpace(input.Details)
	if !validID(actor) || !validID(requestID) || !validReportReason(input.Reason) || len([]rune(input.Details)) > 1000 || !utf8.ValidString(input.Details) || strings.ContainsRune(input.Details, 0) {
		return Report{}, ErrInvalid
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return Report{}, err
	}
	return s.repo.Report(ctx, actor, requestID, input)
}

func (s *Service) ListReports(ctx context.Context, actor string, options Options) (ReportPage, error) {
	if !validID(actor) {
		return ReportPage{}, ErrForbidden
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Status == "" {
		options.Status = "pending"
	}
	if options.Limit < 1 || options.Limit > 100 || (options.Status != "pending" && options.Status != "resolved" && options.Status != "dismissed" && options.Status != "expired") || options.Cursor != "" {
		return ReportPage{}, ErrInvalid
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return ReportPage{}, err
	}
	items, err := s.repo.ListReports(ctx, actor, options)
	if err != nil {
		return ReportPage{}, err
	}
	return ReportPage{Items: items}, nil
}

func (s *Service) ReviewReport(ctx context.Context, actor, reportID string, input ReviewInput) (bool, error) {
	input.Decision = strings.TrimSpace(input.Decision)
	input.Reason = strings.TrimSpace(input.Reason)
	if !validID(actor) || !validID(reportID) || (input.Decision != "hide" && input.Decision != "dismiss") || !utf8.ValidString(input.Reason) || len([]rune(input.Reason)) < 3 || len([]rune(input.Reason)) > 500 || strings.ContainsRune(input.Reason, 0) {
		return false, ErrInvalid
	}
	if _, err := s.repo.Expire(ctx, s.now().UTC(), 500); err != nil {
		return false, err
	}
	return s.repo.ReviewReport(ctx, actor, reportID, input, s.now().UTC())
}

func validReportReason(reason string) bool {
	switch reason {
	case "safety", "spam", "inappropriate", "other":
		return true
	default:
		return false
	}
}

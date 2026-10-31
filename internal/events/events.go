// Package events implements church-wide published events and member RSVPs.
package events

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	_ "time/tzdata"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/google/uuid"
)

var (
	ErrForbidden          = errors.New("event action forbidden")
	ErrNotFound           = errors.New("event not found")
	ErrInvalid            = errors.New("invalid event input")
	ErrConflict           = errors.New("event conflict")
	ErrVersionConflict    = errors.New("event version conflict")
	ErrFullCapacity       = errors.New("event is at full capacity")
	ErrRegistrationClosed = errors.New("event registration is closed")
)

const maxAttendeesPerRSVP = 10

type Event struct {
	GroupID         string    `json:"groupId,omitempty"`
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Location        string    `json:"location"`
	StartsAt        time.Time `json:"startsAt"`
	TimeZone        string    `json:"timeZone"`
	Capacity        *int      `json:"capacity"`
	AttendeeCount   int       `json:"attendeeCount"`
	MyAttendeeCount int       `json:"myAttendeeCount,omitempty"`
	Status          string    `json:"status"`
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type RSVP struct {
	EventID       string    `json:"eventId"`
	AttendeeCount int       `json:"attendeeCount"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Coordinator struct {
	EventID    string    `json:"eventId"`
	GroupID    string    `json:"groupId"`
	GroupName  string    `json:"groupName"`
	AccountID  string    `json:"accountId"`
	FirstName  string    `json:"firstName"`
	LastName   string    `json:"lastName"`
	AssignedAt time.Time `json:"assignedAt"`
}

type CreateInput struct {
	GroupID     string    `json:"groupId,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartsAt    time.Time `json:"startsAt"`
	TimeZone    string    `json:"timeZone"`
	Capacity    *int      `json:"capacity"`
}

type UpdateInput struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartsAt    time.Time `json:"startsAt"`
	TimeZone    string    `json:"timeZone"`
	Capacity    *int      `json:"capacity"`
	Status      string    `json:"status"`
	Version     int       `json:"version"`
}

type Repository struct{ db *sql.DB }
type Service struct {
	db   *sql.DB
	repo *Repository
	now  func() time.Time
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func NewService(repo *Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: repo.db, repo: repo, now: now}
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func normalizeInput(in CreateInput) (CreateInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Location = strings.TrimSpace(in.Location)
	if (in.GroupID != "" && !validID(in.GroupID)) || !utf8.ValidString(in.Title) || !utf8.ValidString(in.Description) || !utf8.ValidString(in.Location) ||
		strings.ContainsRune(in.Title, 0) || strings.ContainsRune(in.Description, 0) || strings.ContainsRune(in.Location, 0) ||
		utf8.RuneCountInString(in.Title) < 1 || utf8.RuneCountInString(in.Title) > 160 ||
		utf8.RuneCountInString(in.Description) > 5000 || utf8.RuneCountInString(in.Location) > 300 ||
		in.StartsAt.IsZero() || strings.TrimSpace(in.TimeZone) != in.TimeZone {
		return CreateInput{}, ErrInvalid
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		return CreateInput{}, ErrInvalid
	}
	if in.Capacity != nil && (*in.Capacity < 1 || *in.Capacity > 100000) {
		return CreateInput{}, ErrInvalid
	}
	in.StartsAt = in.StartsAt.UTC()
	return in, nil
}

func validateUpdate(in UpdateInput) (UpdateInput, error) {
	base, err := normalizeInput(CreateInput{Title: in.Title, Description: in.Description, Location: in.Location, StartsAt: in.StartsAt, TimeZone: in.TimeZone, Capacity: in.Capacity})
	if err != nil || in.Version < 1 || (in.Status != "draft" && in.Status != "published" && in.Status != "cancelled") {
		return UpdateInput{}, ErrInvalid
	}
	in.Title, in.Description, in.Location, in.StartsAt, in.TimeZone, in.Capacity = base.Title, base.Description, base.Location, base.StartsAt, base.TimeZone, base.Capacity
	return in, nil
}

func (s *Service) checkManager(ctx context.Context, actor string, action func(*sql.Tx) error) error {
	if !validID(actor) {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = access.CheckTx(ctx, tx, actor, "events.manage", ""); err != nil {
		if errors.Is(err, access.ErrForbidden) {
			return ErrForbidden
		}
		return err
	}
	if err = action(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Create(ctx context.Context, actor string, raw CreateInput) (Event, error) {
	if !validID(actor) {
		return Event{}, ErrForbidden
	}
	var result Event
	err := s.checkGroupManager(ctx, actor, raw.GroupID, "", func(tx *sql.Tx) error {
		in, e := normalizeInput(raw)
		if e != nil {
			return e
		}
		result, e = s.repo.createTx(ctx, tx, actor, in, s.now())
		return e
	})
	return result, err
}

func (s *Service) Update(ctx context.Context, actor, eventID string, raw UpdateInput) (Event, error) {
	if !validID(eventID) {
		return Event{}, ErrNotFound
	}
	if !validID(actor) {
		return Event{}, ErrForbidden
	}
	var result Event
	err := s.checkGroupManager(ctx, actor, "", eventID, func(tx *sql.Tx) error {
		in, e := validateUpdate(raw)
		if e != nil {
			return e
		}
		result, e = s.repo.updateTx(ctx, tx, actor, eventID, in, s.now())
		return e
	})
	return result, err
}

func (s *Service) AdminList(ctx context.Context, actor string) ([]Event, error) {
	if !validID(actor) {
		return nil, ErrForbidden
	}
	var result []Event
	err := s.checkManager(ctx, actor, func(tx *sql.Tx) error {
		var err error
		result, err = s.repo.listTx(ctx, tx, "", s.now(), true)
		return err
	})
	return result, err
}

func (s *Service) List(ctx context.Context, viewer string) ([]Event, error) {
	if viewer != "" && !validID(viewer) {
		return nil, ErrForbidden
	}
	return s.repo.List(ctx, viewer, s.now())
}

func (s *Service) Detail(ctx context.Context, viewer, eventID string) (Event, error) {
	if viewer != "" && !validID(viewer) {
		return Event{}, ErrForbidden
	}
	if !validID(eventID) {
		return Event{}, ErrNotFound
	}
	return s.repo.Detail(ctx, eventID, viewer, s.now())
}

func (s *Service) RSVP(ctx context.Context, actor, eventID string, attendees int) (RSVP, error) {
	if !validID(actor) {
		return RSVP{}, ErrForbidden
	}
	if !validID(eventID) || attendees < 1 || attendees > maxAttendeesPerRSVP {
		return RSVP{}, ErrInvalid
	}
	return s.repo.RSVP(ctx, eventID, actor, attendees, s.now())
}

func (s *Service) CancelRSVP(ctx context.Context, actor, eventID string) (bool, error) {
	if !validID(actor) {
		return false, ErrForbidden
	}
	if !validID(eventID) {
		return false, ErrNotFound
	}
	return s.repo.CancelRSVP(ctx, eventID, actor, s.now())
}

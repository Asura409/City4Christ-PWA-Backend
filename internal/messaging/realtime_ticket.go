package messaging

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"
)

const realtimeTicketLifetime = 30 * time.Second

var ErrInvalidRealtimeTicket = errors.New("invalid realtime ticket")

type RealtimeTicket struct {
	Value     string
	ExpiresAt time.Time
}

type RealtimeTicketIdentity struct {
	AccountID       string
	SessionID       string
	AccessExpiresAt time.Time
}

type RealtimeTicketService struct {
	repo *Repository
	now  func() time.Time
}

func NewRealtimeTicketService(repo *Repository, now func() time.Time) *RealtimeTicketService {
	if now == nil {
		now = time.Now
	}
	return &RealtimeTicketService{repo: repo, now: now}
}

func (s *RealtimeTicketService) Issue(ctx context.Context, accountID, sessionID string, accessExpiresAt time.Time) (RealtimeTicket, error) {
	if !validID(accountID) || !validID(sessionID) {
		return RealtimeTicket{}, ErrInvalid
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return RealtimeTicket{}, err
	}
	now := s.now().UTC()
	accessExpiresAt = accessExpiresAt.UTC()
	if !accessExpiresAt.After(now) {
		return RealtimeTicket{}, ErrInvalid
	}
	ticket := RealtimeTicket{Value: base64.RawURLEncoding.EncodeToString(secret), ExpiresAt: now.Add(realtimeTicketLifetime)}
	digest := sha256.Sum256([]byte(ticket.Value))
	if err := s.repo.issueRealtimeTicket(ctx, digest[:], accountID, sessionID, accessExpiresAt, now, ticket.ExpiresAt); err != nil {
		return RealtimeTicket{}, err
	}
	return ticket, nil
}

// Consume atomically invalidates a ticket after the first successful claim.
func (s *RealtimeTicketService) Consume(ctx context.Context, value string) (RealtimeTicketIdentity, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return RealtimeTicketIdentity{}, ErrInvalidRealtimeTicket
	}
	digest := sha256.Sum256([]byte(value))
	identity, err := s.repo.consumeRealtimeTicket(ctx, digest[:], s.now().UTC())
	if errors.Is(err, ErrNotFound) {
		return RealtimeTicketIdentity{}, ErrInvalidRealtimeTicket
	}
	return identity, err
}

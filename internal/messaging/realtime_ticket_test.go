package messaging

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRealtimeTicketsAreOpaqueSingleUseAndExpire(t *testing.T) {
	db, seed := messagingDB(t)
	accountID := seed("realtime-ticket@example.invalid")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	sessionID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO auth_sessions(id,account_id,expires_at,created_at) VALUES($1,$2,$3,$4)`, sessionID, accountID, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	service := NewRealtimeTicketService(NewRepository(db), func() time.Time { return now })
	ticket, err := service.Issue(context.Background(), accountID, sessionID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(ticket.Value) != 43 || !ticket.ExpiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("ticket = %+v, want a 32-byte opaque secret with 30-second lifetime", ticket)
	}
	var stored []byte
	if err := db.QueryRow(`SELECT token_hash FROM realtime_tickets`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(ticket.Value))
	if string(stored) != string(want[:]) {
		t.Fatal("database did not store only the SHA-256 ticket digest")
	}
	got, err := service.Consume(context.Background(), ticket.Value)
	if err != nil || got.AccountID != accountID || got.SessionID != sessionID || !got.AccessExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("first consume = (%+v, %v), want account/session identity", got, err)
	}
	if _, err := service.Consume(context.Background(), ticket.Value); err != ErrInvalidRealtimeTicket {
		t.Fatalf("replay consume err = %v, want invalid ticket", err)
	}
	expired, err := service.Issue(context.Background(), accountID, sessionID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if _, err := service.Consume(context.Background(), expired.Value); err != ErrInvalidRealtimeTicket {
		t.Fatalf("expired ticket err = %v, want invalid ticket", err)
	}
	now = now.Add(-time.Minute)
	revoked, err := service.Issue(context.Background(), accountID, sessionID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE auth_sessions SET revoked_at=$2 WHERE id=$1`, sessionID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Consume(context.Background(), revoked.Value); err != ErrInvalidRealtimeTicket {
		t.Fatalf("revoked-session ticket err = %v, want invalid ticket", err)
	}
}

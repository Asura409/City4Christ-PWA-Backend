package messaging

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"
)

func TestRealtimeTicketsAreOpaqueSingleUseAndExpire(t *testing.T) {
	db, seed := messagingDB(t)
	accountID := seed("realtime-ticket@example.invalid")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	service := NewRealtimeTicketService(NewRepository(db), func() time.Time { return now })
	ticket, err := service.Issue(context.Background(), accountID)
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
	if err != nil || got != accountID {
		t.Fatalf("first consume = (%q, %v), want account %q", got, err, accountID)
	}
	if _, err := service.Consume(context.Background(), ticket.Value); err != ErrInvalidRealtimeTicket {
		t.Fatalf("replay consume err = %v, want invalid ticket", err)
	}
	expired, err := service.Issue(context.Background(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if _, err := service.Consume(context.Background(), expired.Value); err != ErrInvalidRealtimeTicket {
		t.Fatalf("expired ticket err = %v, want invalid ticket", err)
	}
}

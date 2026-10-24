package messaging

import (
	"context"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"testing"
)

func TestServiceRequiresVerifiedAccountsAndValidOneToOneInputs(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	verified := func(email string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Verified','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := verified("service-first@example.invalid")
	second := verified("service-second@example.invalid")
	var unverified string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name) VALUES('service-unverified@example.invalid','Unverified','Member') RETURNING id`).Scan(&unverified); err != nil {
		t.Fatal(err)
	}
	s := NewService(NewRepository(db), nil)
	ctx := context.Background()
	if _, _, err := s.CreateConversation(ctx, first, unverified); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unverified recipient err = %v", err)
	}
	if _, _, err := s.CreateConversation(ctx, unverified, second); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unverified actor err = %v", err)
	}
	if _, _, err := s.CreateConversation(ctx, first, first); !errors.Is(err, ErrInvalid) {
		t.Fatalf("self conversation err = %v", err)
	}
	if _, _, err := s.CreateConversation(ctx, "not-a-uuid", second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid account ID err = %v", err)
	}
	conversation, _, err := s.CreateConversation(ctx, first, second)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, body string }{{"not-a-uuid", "Hello"}, {"7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", "   "}, {"7219a6ea-b436-48ca-9d75-b2b8f5dd18ef", string(make([]byte, maxMessageRunes+1))}} {
		if _, _, err := s.SendMessage(ctx, first, conversation.ID, tc.id, tc.body); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid send (%q,%d chars) err = %v", tc.id, len(tc.body), err)
		}
	}
	if _, _, err := s.SendMessage(ctx, unverified, conversation.ID, "300ebbe1-01ca-4c47-a340-03cedf07359e", "No session"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unverified sender err = %v", err)
	}
}

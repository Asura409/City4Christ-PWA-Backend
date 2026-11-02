package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestRealtimePresenceIsScopedToSubscribedDirectConversation(t *testing.T) {
	db, sessions, senderID, senderAccess := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var recipientID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('presence-recipient@example.invalid','Presence','Member',now()) RETURNING id`).Scan(&recipientID); err != nil {
		t.Fatal(err)
	}
	recipientTokens, err := sessions.Start(ctx, recipientID)
	if err != nil {
		t.Fatal(err)
	}
	service := messaging.NewService(messaging.NewRepository(db), nil)
	conversation, _, err := service.CreateConversation(ctx, senderID, recipientID)
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://pwa.example.invalid"
	server := httptest.NewServer(New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{origin}}))
	defer server.Close()
	open := func(access string) *websocket.Conn {
		t.Helper()
		actor, sessionID, expiresAt, err := sessions.AuthenticateSession(ctx, access)
		if err != nil {
			t.Fatal(err)
		}
		ticket, err := messaging.NewRealtimeTicketService(messaging.NewRepository(db), nil).Issue(ctx, actor.ID, sessionID, expiresAt)
		if err != nil {
			t.Fatal(err)
		}
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/realtime", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Write(ctx, conn, map[string]string{"type": "authenticate", "ticket": ticket.Value}); err != nil {
			t.Fatal(err)
		}
		var ready map[string]string
		if err := wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
			t.Fatalf("ready = %v, err = %v", ready, err)
		}
		return conn
	}
	sender := open(senderAccess)
	defer sender.CloseNow()
	subscribe := map[string]string{"type": "presence.subscribe", "scope": "direct", "conversationId": conversation.ID}
	if err := wsjson.Write(ctx, sender, subscribe); err != nil {
		t.Fatal(err)
	}
	var initial map[string]any
	if err := wsjson.Read(ctx, sender, &initial); err != nil {
		t.Fatal(err)
	}
	if initial["type"] != "presence.snapshot" || initial["scope"] != "direct" || initial["conversationId"] != conversation.ID || initial["senderId"] != recipientID || initial["online"] != false {
		t.Fatalf("initial direct presence snapshot = %#v", initial)
	}

	recipient := open(recipientTokens.AccessToken)
	if err := wsjson.Read(ctx, sender, &initial); err != nil {
		t.Fatal(err)
	}
	if initial["type"] != "presence.online" || initial["senderId"] != recipientID || initial["online"] != true {
		t.Fatalf("recipient online event = %#v", initial)
	}
	if err := wsjson.Write(ctx, recipient, subscribe); err != nil {
		t.Fatal(err)
	}
	var recipientSnapshot map[string]any
	if err := wsjson.Read(ctx, recipient, &recipientSnapshot); err != nil {
		t.Fatal(err)
	}
	if recipientSnapshot["type"] != "presence.snapshot" || recipientSnapshot["senderId"] != senderID || recipientSnapshot["online"] != true {
		t.Fatalf("recipient presence snapshot = %#v", recipientSnapshot)
	}
	recipient.CloseNow()
	var offline map[string]any
	if err := wsjson.Read(ctx, sender, &offline); err != nil {
		t.Fatal(err)
	}
	if offline["type"] != "presence.offline" || offline["senderId"] != recipientID || offline["online"] != false {
		t.Fatalf("recipient offline event = %#v", offline)
	}
}

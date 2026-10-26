package app

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func realtimeFixture(t *testing.T) (*sql.DB, *auth.SessionService, string, string) {
	t.Helper()
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{73}, 32)
	sessions, err := auth.NewSessionService(db, auth.SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	var accountID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('realtime-ws@example.invalid','Real','Time',now()) RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	return db, sessions, accountID, tokens.AccessToken
}

func TestRealtimeWebSocketAuthenticatesTicketBeforeReady(t *testing.T) {
	db, sessions, accountID, accessToken := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	origin := "https://pwa.example.invalid"
	_, sessionID, accessExpiresAt, err := sessions.AuthenticateSession(ctx, accessToken)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := messaging.NewRealtimeTicketService(messaging.NewRepository(db), nil).Issue(ctx, accountID, sessionID, accessExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	h := New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{origin}})
	server := httptest.NewServer(h)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/realtime"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}}})
	if err != nil {
		t.Fatalf("open realtime socket: %v", err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "authenticate", "ticket": ticket.Value}); err != nil {
		t.Fatalf("authenticate socket: %v", err)
	}
	var ready struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &ready); err != nil {
		t.Fatalf("read ready frame: %v", err)
	}
	if ready.Type != "ready" {
		t.Fatalf("first server frame type = %q, want ready", ready.Type)
	}
}

func TestRealtimeWebSocketRejectsTicketReplayAndForeignOrigin(t *testing.T) {
	db, sessions, accountID, accessToken := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, sessionID, expiresAt, err := sessions.AuthenticateSession(ctx, accessToken)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := messaging.NewRealtimeTicketService(messaging.NewRepository(db), nil).Issue(ctx, accountID, sessionID, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	allowedOrigin := "https://pwa.example.invalid"
	server := httptest.NewServer(New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{allowedOrigin}}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/realtime"
	if _, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://attacker.example.invalid"}}}); err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-origin dial = response %v, err %v; want HTTP 403", response, err)
	}
	options := &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{allowedOrigin}}}
	conn, _, err := websocket.Dial(ctx, wsURL, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "authenticate", "ticket": ticket.Value}); err != nil {
		t.Fatal(err)
	}
	var ready map[string]string
	if err := wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("first ticket authentication = %v, err %v", ready, err)
	}
	conn.CloseNow()

	replay, _, err := websocket.Dial(ctx, wsURL, options)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.CloseNow()
	if err := wsjson.Write(ctx, replay, map[string]string{"type": "authenticate", "ticket": ticket.Value}); err != nil {
		t.Fatal(err)
	}
	var frame map[string]string
	if err := wsjson.Read(ctx, replay, &frame); err == nil {
		t.Fatalf("replayed ticket unexpectedly received server frame: %v", frame)
	}
}

func TestRealtimeDirectMessageDeliveredAsMetadataAfterRESTSend(t *testing.T) {
	db, sessions, senderID, senderAccess := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var recipientID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('realtime-recipient@example.invalid','Receive','Message',now()) RETURNING id`).Scan(&recipientID); err != nil {
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
	h := New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{origin}})
	server := httptest.NewServer(h)
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
	connections := []*websocket.Conn{open(senderAccess), open(recipientTokens.AccessToken)}
	defer func() {
		for _, conn := range connections {
			conn.CloseNow()
		}
	}()
	payload := fmt.Sprintf(`{"clientMessageId":"%s","body":"this private body stays in REST"}`, "35b3da77-4211-46e8-84b7-dac881208bab")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/conversations/"+conversation.ID+"/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+senderAccess)
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("send status = %d, want 201", response.StatusCode)
	}

	// Each participant receives identifiers and the sequence; message content remains in REST history.
	for _, conn := range connections {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("read realtime event: %v", err)
		}
		if event["type"] != "message.created" || event["scope"] != "direct" || event["conversationId"] != conversation.ID {
			t.Fatalf("unexpected realtime event: %#v", event)
		}
		if _, hasBody := event["body"]; hasBody {
			t.Fatalf("realtime event must not include message body: %#v", event)
		}
	}
}

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
	var messageID string
	for _, conn := range connections {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("read realtime event: %v", err)
		}
		if event["type"] != "message.created" || event["scope"] != "direct" || event["conversationId"] != conversation.ID {
			t.Fatalf("unexpected realtime event: %#v", event)
		}
		messageID, _ = event["messageId"].(string)
		if _, hasBody := event["body"]; hasBody {
			t.Fatalf("realtime event must not include message body: %#v", event)
		}
	}
	mutate := func(method, payload string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+"/api/v1/conversations/"+conversation.ID+"/messages/"+messageID, strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+senderAccess)
		req.Header.Set("Origin", origin)
		if payload != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	updated := mutate(http.MethodPatch, `{"body":"corrected private message"}`)
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("edit status = %d", updated.StatusCode)
	}
	updated.Body.Close()
	for _, conn := range connections {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil || event["type"] != "message.updated" || event["messageId"] != messageID {
			t.Fatalf("edit realtime event = %#v, err=%v", event, err)
		}
		if _, hasBody := event["body"]; hasBody {
			t.Fatalf("edit realtime event leaked body: %#v", event)
		}
	}
	deleted := mutate(http.MethodDelete, "")
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}
	deleted.Body.Close()
	for _, conn := range connections {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil || event["type"] != "message.deleted" || event["messageId"] != messageID {
			t.Fatalf("delete realtime event = %#v, err=%v", event, err)
		}
		if _, hasBody := event["body"]; hasBody {
			t.Fatalf("delete realtime event leaked body: %#v", event)
		}
	}
}

func TestRealtimeTypingSignalIsParticipantScopedAndContainsNoMessageContent(t *testing.T) {
	db, sessions, senderID, senderAccess := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var recipientID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('realtime-typing-recipient@example.invalid','Receive','Typing',now()) RETURNING id`).Scan(&recipientID); err != nil {
		t.Fatal(err)
	}
	recipientTokens, err := sessions.Start(ctx, recipientID)
	if err != nil {
		t.Fatal(err)
	}
	var outsiderID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('realtime-typing-outsider@example.invalid','Outside','Member',now()) RETURNING id`).Scan(&outsiderID); err != nil {
		t.Fatal(err)
	}
	outsiderTokens, err := sessions.Start(ctx, outsiderID)
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
	sender, recipient, outsider := open(senderAccess), open(recipientTokens.AccessToken), open(outsiderTokens.AccessToken)
	defer sender.CloseNow()
	defer recipient.CloseNow()
	defer outsider.CloseNow()
	if err := wsjson.Write(ctx, sender, map[string]string{"type": "typing.start", "scope": "direct", "conversationId": conversation.ID}); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := wsjson.Read(ctx, recipient, &event); err != nil {
		t.Fatalf("read typing signal: %v", err)
	}
	if event["type"] != "typing.start" || event["scope"] != "direct" || event["conversationId"] != conversation.ID {
		t.Fatalf("typing event = %#v", event)
	}
	if _, exists := event["body"]; exists {
		t.Fatalf("typing signal unexpectedly includes content: %#v", event)
	}
	quietCtx, quietCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer quietCancel()
	var privateEcho map[string]any
	if err := wsjson.Read(quietCtx, sender, &privateEcho); err == nil {
		t.Fatalf("sender received own typing event: %#v", privateEcho)
	}
	if err := wsjson.Write(ctx, outsider, map[string]string{"type": "typing.start", "scope": "direct", "conversationId": conversation.ID}); err != nil {
		t.Fatal(err)
	}
	unauthorizedCtx, unauthorizedCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer unauthorizedCancel()
	var unauthorizedEvent map[string]any
	if err := wsjson.Read(unauthorizedCtx, recipient, &unauthorizedEvent); err == nil {
		t.Fatalf("nonparticipant typing event leaked: %#v", unauthorizedEvent)
	}
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const realtimeAuthTimeout = 5 * time.Second

type realtimeAuthFrame struct {
	Type   string `json:"type"`
	Ticket string `json:"ticket"`
}

type realtimeClientFrame struct {
	Type string `json:"type"`
}

func realtimeSocketRoutes(mux *http.ServeMux, tickets *messaging.RealtimeTicketService, sessions *auth.SessionService, hub *messaging.RealtimeHub, c Config) {
	mux.HandleFunc("GET /api/v1/realtime", func(w http.ResponseWriter, r *http.Request) {
		if tickets == nil || sessions == nil || hub == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Realtime messaging is not configured.", nil)
			return
		}
		if r.URL.RawQuery != "" || len(r.Header.Values("Origin")) != 1 || !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply one allowed Origin and no query parameters.", nil)
			return
		}
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil {
			writeError(w, 403, "origin_not_allowed", "Supply one allowed Origin and no query parameters.", nil)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{origin.Host}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(2048)
		authCtx, cancelAuth := context.WithTimeout(r.Context(), realtimeAuthTimeout)
		var frame realtimeAuthFrame
		err = wsjson.Read(authCtx, conn, &frame)
		cancelAuth()
		if err != nil || frame.Type != "authenticate" || frame.Ticket == "" {
			_ = conn.Close(websocket.StatusPolicyViolation, "ticket authentication required")
			return
		}
		identity, err := tickets.Consume(r.Context(), frame.Ticket)
		if err != nil {
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid or expired ticket")
			return
		}
		active, err := sessions.SessionActive(r.Context(), identity.AccountID, identity.SessionID)
		if err != nil || !active || !identity.AccessExpiresAt.After(timeNow(c)) {
			_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
			return
		}
		client, err := hub.Register(identity.AccountID)
		if errors.Is(err, messaging.ErrRealtimeCapacity) {
			_ = conn.Close(websocket.StatusTryAgainLater, "reconnect after capacity is available")
			return
		}
		if err != nil {
			_ = conn.Close(websocket.StatusInternalError, "realtime unavailable")
			return
		}
		defer hub.Unregister(client)
		ctx, cancel := context.WithDeadline(r.Context(), identity.AccessExpiresAt)
		defer cancel()
		if err := wsjson.Write(ctx, conn, map[string]string{"type": "ready"}); err != nil {
			return
		}
		writerDone := make(chan struct{})
		var writeMu sync.Mutex
		go realtimeSocketWriter(ctx, cancel, conn, client, writerDone, &writeMu)
		go monitorRealtimeSession(ctx, cancel, conn, sessions, identity.AccountID, identity.SessionID)
		for {
			var incoming realtimeClientFrame
			if err := wsjson.Read(ctx, conn, &incoming); err != nil {
				break
			}
			if incoming.Type != "ping" {
				_ = conn.Close(websocket.StatusPolicyViolation, "message writes use the REST API")
				break
			}
			writeMu.Lock()
			err := wsjson.Write(ctx, conn, map[string]string{"type": "pong"})
			writeMu.Unlock()
			if err != nil {
				break
			}
		}
		cancel()
		<-writerDone
	})
}

func realtimeSocketWriter(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, client *messaging.RealtimeClient, done chan<- struct{}, writeMu *sync.Mutex) {
	defer close(done)
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.CloseNow()
			return
		case <-client.Done():
			_ = conn.Close(websocket.StatusTryAgainLater, "reconnect and catch up through REST history")
			cancel()
			return
		case payload := <-client.Events():
			writeMu.Lock()
			err := wsjson.Write(ctx, conn, json.RawMessage(payload))
			writeMu.Unlock()
			if err != nil {
				cancel()
				return
			}
		case <-keepalive.C:
			writeMu.Lock()
			err := conn.Ping(ctx)
			writeMu.Unlock()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func monitorRealtimeSession(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, sessions *auth.SessionService, accountID, sessionID string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, checkCancel := context.WithTimeout(context.Background(), 2*time.Second)
			active, err := sessions.SessionActive(checkCtx, accountID, sessionID)
			checkCancel()
			if err != nil || !active {
				conn.CloseNow()
				cancel()
				return
			}
		}
	}
}

func publishDirectMessage(ctx context.Context, service *messaging.Service, hub *messaging.RealtimeHub, message messaging.Message) {
	accounts, err := service.DirectRealtimeRecipients(ctx, message.ConversationID)
	if err != nil {
		slog.Warn("realtime direct message fanout skipped; REST history remains available", "conversation_id", message.ConversationID, "error", err)
		return
	}
	hub.Publish(accounts, messaging.RealtimeEvent{Type: "message.created", Scope: "direct", ConversationID: message.ConversationID, MessageID: message.ID, Sequence: message.Sequence})
}

func publishGroupMessage(ctx context.Context, service *messaging.Service, hub *messaging.RealtimeHub, message messaging.GroupMessage) {
	accounts, err := service.GroupRealtimeRecipients(ctx, message.GroupID)
	if err != nil {
		slog.Warn("realtime group message fanout skipped; REST history remains available", "group_id", message.GroupID, "error", err)
		return
	}
	hub.Publish(accounts, messaging.RealtimeEvent{Type: "message.created", Scope: "group", GroupID: message.GroupID, MessageID: message.ID, Sequence: message.Sequence})
}

func publishGroupMessageDeleted(ctx context.Context, service *messaging.Service, hub *messaging.RealtimeHub, groupID, messageID string) {
	accounts, err := service.GroupRealtimeRecipients(ctx, groupID)
	if err != nil {
		slog.Warn("realtime group deletion fanout skipped; REST history remains available", "group_id", groupID, "error", err)
		return
	}
	hub.Publish(accounts, messaging.RealtimeEvent{Type: "message.deleted", Scope: "group", GroupID: groupID, MessageID: messageID})
}

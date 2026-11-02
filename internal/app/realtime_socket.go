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
	"github.com/google/uuid"
)

const realtimeAuthTimeout = 5 * time.Second

type realtimeAuthFrame struct {
	Type   string `json:"type"`
	Ticket string `json:"ticket"`
}

type realtimeClientFrame struct {
	Type           string `json:"type"`
	Scope          string `json:"scope"`
	ConversationID string `json:"conversationId"`
	GroupID        string `json:"groupId"`
}

func realtimeSocketRoutes(mux *http.ServeMux, tickets *messaging.RealtimeTicketService, service *messaging.Service, sessions *auth.SessionService, hub *messaging.RealtimeHub, c Config) {
	mux.HandleFunc("GET /api/v1/realtime", func(w http.ResponseWriter, r *http.Request) {
		if tickets == nil || service == nil || sessions == nil || hub == nil || !validOrigin(c.AppOrigin) {
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
		client, firstConnection, err := hub.RegisterWithStatus(identity.AccountID)
		if errors.Is(err, messaging.ErrRealtimeCapacity) {
			_ = conn.Close(websocket.StatusTryAgainLater, "reconnect after capacity is available")
			return
		}
		if err != nil {
			_ = conn.Close(websocket.StatusInternalError, "realtime unavailable")
			return
		}
		defer func() {
			if last := hub.Unregister(client); last {
				presenceCtx, presenceCancel := context.WithTimeout(context.Background(), 2*time.Second)
				publishRealtimePresence(presenceCtx, service, hub, identity.AccountID, false)
				presenceCancel()
			}
		}()
		ctx, cancel := context.WithDeadline(r.Context(), identity.AccessExpiresAt)
		defer cancel()
		if err := wsjson.Write(ctx, conn, map[string]string{"type": "ready"}); err != nil {
			return
		}
		writerDone := make(chan struct{})
		var writeMu sync.Mutex
		go realtimeSocketWriter(ctx, cancel, conn, client, writerDone, &writeMu)
		go monitorRealtimeSession(ctx, cancel, conn, sessions, identity.AccountID, identity.SessionID)
		if firstConnection {
			publishRealtimePresence(ctx, service, hub, identity.AccountID, true)
		}
		var lastTypingEvent time.Time
		for {
			var incoming realtimeClientFrame
			if err := wsjson.Read(ctx, conn, &incoming); err != nil {
				break
			}
			if incoming.Type == "typing.start" || incoming.Type == "typing.stop" {
				now := timeNow(c)
				if !lastTypingEvent.IsZero() && now.Sub(lastTypingEvent) < 300*time.Millisecond {
					continue
				}
				target, ok := realtimePresenceTarget(ctx, service, identity.AccountID, incoming)
				if ok {
					lastTypingEvent = now
					hub.PublishExcept(target.Members, identity.AccountID, messaging.RealtimeEvent{Type: incoming.Type, Scope: incoming.Scope, ConversationID: incoming.ConversationID, GroupID: incoming.GroupID, SenderID: identity.AccountID})
				}
				continue
			}
			if incoming.Type == "presence.subscribe" {
				if target, ok := realtimePresenceTarget(ctx, service, identity.AccountID, incoming); ok {
					hub.SubscribePresence(client, target)
				} else if scope, id := realtimeFrameTarget(incoming); validRealtimeTargetID(id) {
					hub.UnsubscribePresence(client, scope, id)
				}
				continue
			}
			if incoming.Type == "presence.unsubscribe" {
				scope, id := realtimeFrameTarget(incoming)
				if validRealtimeTargetID(id) && (scope == "direct" || scope == "group") {
					hub.UnsubscribePresence(client, scope, id)
				}
				continue
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

func realtimePresenceTarget(ctx context.Context, service *messaging.Service, actor string, frame realtimeClientFrame) (messaging.RealtimePresenceTarget, bool) {
	scope, id := realtimeFrameTarget(frame)
	var accounts []string
	var err error
	switch scope {
	case "direct":
		accounts, err = service.DirectRealtimeRecipients(ctx, id)
	case "group":
		accounts, err = service.GroupRealtimeRecipients(ctx, id)
	default:
		return messaging.RealtimePresenceTarget{}, false
	}
	if !validRealtimeTargetID(id) || err != nil {
		return messaging.RealtimePresenceTarget{}, false
	}
	for _, accountID := range accounts {
		if accountID == actor {
			return messaging.RealtimePresenceTarget{Scope: scope, ID: id, Members: accounts}, true
		}
	}
	return messaging.RealtimePresenceTarget{}, false
}

func validRealtimeTargetID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func realtimeFrameTarget(frame realtimeClientFrame) (scope, id string) {
	if frame.Scope == "direct" && frame.GroupID == "" {
		return frame.Scope, frame.ConversationID
	}
	if frame.Scope == "group" && frame.ConversationID == "" {
		return frame.Scope, frame.GroupID
	}
	return "", ""
}

func publishRealtimePresence(ctx context.Context, service *messaging.Service, hub *messaging.RealtimeHub, accountID string, online bool) {
	for _, target := range hub.PresenceTargetsForAccount(accountID) {
		var members []string
		var err error
		if target.Scope == "direct" {
			members, err = service.DirectRealtimeRecipients(ctx, target.ID)
		} else if target.Scope == "group" {
			members, err = service.GroupRealtimeRecipients(ctx, target.ID)
		} else {
			continue
		}
		if err != nil {
			slog.Warn("realtime presence fanout skipped", "scope", target.Scope, "target_id", target.ID, "error", err)
			continue
		}
		hub.PublishPresence(target, members, accountID, online)
	}
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

func publishDirectMessageMutation(ctx context.Context, service *messaging.Service, hub *messaging.RealtimeHub, eventType, conversationID, messageID string, sequence int64) {
	accounts, err := service.DirectRealtimeRecipients(ctx, conversationID)
	if err != nil {
		slog.Warn("realtime direct message mutation fanout skipped; REST history remains available", "conversation_id", conversationID, "error", err)
		return
	}
	hub.Publish(accounts, messaging.RealtimeEvent{Type: eventType, Scope: "direct", ConversationID: conversationID, MessageID: messageID, Sequence: sequence})
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

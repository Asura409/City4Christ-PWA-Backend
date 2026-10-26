package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
)

type createConversationRequest struct {
	RecipientID string `json:"recipientId"`
}
type sendMessageRequest struct {
	ClientMessageID string `json:"clientMessageId"`
	Body            string `json:"body"`
}
type markReadRequest struct {
	ThroughMessageID string `json:"throughMessageId"`
}

func directMessagingRoutes(mux *http.ServeMux, service *messaging.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, hub *messaging.RealtimeHub, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, messaging.ErrInvalid):
			writeError(w, 422, "validation_failed", "Check the conversation, message, or pagination fields.", nil)
		case errors.Is(err, messaging.ErrForbidden):
			writeError(w, 403, "forbidden", "A verified member account is required.", nil)
		case errors.Is(err, messaging.ErrNotFound):
			writeError(w, 404, "not_found", "Conversation not found.", nil)
		case errors.Is(err, messaging.ErrIdempotencyConflict):
			writeError(w, 409, "idempotency_conflict", "That client message ID was already used for different content.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Messaging is not configured.", nil)
			return "", false
		}
		if write && !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return "", false
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return "", false
		}
		accountLimitValue, ipLimitValue := 120, 240
		if write {
			accountLimitValue, ipLimitValue = 60, 120
		}
		if !accountLimit(w, r, limiter, "direct-messaging", actor.ID, accountLimitValue, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, e := limiter.Allow(r.Context(), "ip/direct-messaging", clientIP(r, c), ipLimitValue, time.Minute)
			if !limitResult(w, allowed, e) {
				return "", false
			}
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		items, err := service.ListConversations(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": items})
	})
	mux.HandleFunc("POST /api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		in, ok := decodeJSON[createConversationRequest](w, r)
		if !ok {
			return
		}
		conversation, created, err := service.CreateConversation(r.Context(), actor, in.RecipientID)
		if err != nil {
			fail(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, map[string]any{"data": conversation})
	})
	mux.HandleFunc("GET /api/v1/conversations/{conversationId}/messages", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, messaging.ErrInvalid)
			return
		}
		for key, values := range q {
			if (key != "beforeSequence" && key != "limit") || len(values) != 1 {
				fail(w, messaging.ErrInvalid)
				return
			}
		}
		var before int64
		var limit int
		if q.Has("beforeSequence") {
			before, err = strconv.ParseInt(q.Get("beforeSequence"), 10, 64)
			if err != nil {
				fail(w, messaging.ErrInvalid)
				return
			}
		}
		if q.Has("limit") {
			limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				fail(w, messaging.ErrInvalid)
				return
			}
		}
		page, err := service.ListMessages(r.Context(), actor, r.PathValue("conversationId"), before, limit)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]int64{"nextBeforeSequence": page.NextBeforeSequence}})
	})
	mux.HandleFunc("POST /api/v1/conversations/{conversationId}/messages", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		in, ok := decodeJSON[sendMessageRequest](w, r)
		if !ok {
			return
		}
		message, duplicate, err := service.SendMessage(r.Context(), actor, r.PathValue("conversationId"), in.ClientMessageID, in.Body)
		if err != nil {
			fail(w, err)
			return
		}
		if !duplicate {
			publishDirectMessage(r.Context(), service, hub, message)
		}
		status := http.StatusCreated
		if duplicate {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{"data": message})
	})
	mux.HandleFunc("PUT /api/v1/conversations/{conversationId}/read", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		in, ok := decodeJSON[markReadRequest](w, r)
		if !ok {
			return
		}
		receipt, err := service.MarkRead(r.Context(), actor, r.PathValue("conversationId"), in.ThroughMessageID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": receipt})
	})
	mux.HandleFunc("GET /api/v1/group-conversations", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		items, err := service.ListGroupConversations(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": items})
	})
	mux.HandleFunc("GET /api/v1/groups/{groupId}/messages", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, messaging.ErrInvalid)
			return
		}
		for key, values := range q {
			if (key != "beforeSequence" && key != "limit") || len(values) != 1 {
				fail(w, messaging.ErrInvalid)
				return
			}
		}
		var before int64
		var limit int
		if q.Has("beforeSequence") {
			before, err = strconv.ParseInt(q.Get("beforeSequence"), 10, 64)
			if err != nil {
				fail(w, messaging.ErrInvalid)
				return
			}
		}
		if q.Has("limit") {
			limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				fail(w, messaging.ErrInvalid)
				return
			}
		}
		page, err := service.ListGroupMessages(r.Context(), actor, r.PathValue("groupId"), before, limit)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]int64{"nextBeforeSequence": page.NextBeforeSequence}})
	})
	mux.HandleFunc("POST /api/v1/groups/{groupId}/messages", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		in, ok := decodeJSON[sendMessageRequest](w, r)
		if !ok {
			return
		}
		message, duplicate, err := service.SendGroupMessage(r.Context(), actor, r.PathValue("groupId"), in.ClientMessageID, in.Body)
		if err != nil {
			fail(w, err)
			return
		}
		if !duplicate {
			publishGroupMessage(r.Context(), service, hub, message)
		}
		status := http.StatusCreated
		if duplicate {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{"data": message})
	})
	mux.HandleFunc("PUT /api/v1/groups/{groupId}/read", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		in, ok := decodeJSON[markReadRequest](w, r)
		if !ok {
			return
		}
		receipt, err := service.MarkGroupRead(r.Context(), actor, r.PathValue("groupId"), in.ThroughMessageID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": receipt})
	})
	mux.HandleFunc("DELETE /api/v1/groups/{groupId}/messages/{messageId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, messaging.ErrInvalid)
			return
		}
		deleted, err := service.DeleteGroupMessage(r.Context(), actor, r.PathValue("groupId"), r.PathValue("messageId"))
		if err != nil {
			fail(w, err)
			return
		}
		if deleted {
			publishGroupMessageDeleted(r.Context(), service, hub, r.PathValue("groupId"), r.PathValue("messageId"))
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

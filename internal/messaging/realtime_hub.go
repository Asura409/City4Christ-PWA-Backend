package messaging

import (
	"encoding/json"
	"errors"
	"sync"
)

const (
	defaultRealtimeQueueCapacity = 32
	defaultRealtimePerAccount    = 4
	defaultRealtimeConnections   = 2000
)

var ErrRealtimeCapacity = errors.New("realtime connection capacity reached")

type RealtimeEvent struct {
	Type           string `json:"type"`
	Scope          string `json:"scope"`
	ConversationID string `json:"conversationId,omitempty"`
	GroupID        string `json:"groupId,omitempty"`
	MessageID      string `json:"messageId,omitempty"`
	Sequence       int64  `json:"sequence,omitempty"`
	SenderID       string `json:"senderId,omitempty"`
	Online         *bool  `json:"online,omitempty"`
	OnlineCount    *int   `json:"onlineCount,omitempty"`
}

type RealtimeClient struct {
	accountID string
	queue     chan []byte
	done      chan struct{}
}

func (c *RealtimeClient) AccountID() string     { return c.accountID }
func (c *RealtimeClient) Events() <-chan []byte { return c.queue }
func (c *RealtimeClient) Done() <-chan struct{} { return c.done }

type RealtimeHub struct {
	mu                  sync.Mutex
	clients             map[string]map[*RealtimeClient]struct{}
	presence            map[string]*realtimePresenceEntry
	connections         int
	queueCapacity       int
	maxPerAccount       int
	maxTotalConnections int
}

func NewRealtimeHub() *RealtimeHub {
	return newRealtimeHub(defaultRealtimeQueueCapacity, defaultRealtimePerAccount, defaultRealtimeConnections)
}

func newRealtimeHub(queueCapacity, maxPerAccount, maxTotalConnections int) *RealtimeHub {
	return &RealtimeHub{
		clients:             make(map[string]map[*RealtimeClient]struct{}),
		presence:            make(map[string]*realtimePresenceEntry),
		queueCapacity:       queueCapacity,
		maxPerAccount:       maxPerAccount,
		maxTotalConnections: maxTotalConnections,
	}
}

func (h *RealtimeHub) Register(accountID string) (*RealtimeClient, error) {
	client, _, err := h.RegisterWithStatus(accountID)
	return client, err
}

func (h *RealtimeHub) RegisterWithStatus(accountID string) (*RealtimeClient, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	byAccount := h.clients[accountID]
	if len(byAccount) >= h.maxPerAccount || h.connections >= h.maxTotalConnections {
		return nil, false, ErrRealtimeCapacity
	}
	client := &RealtimeClient{accountID: accountID, queue: make(chan []byte, h.queueCapacity), done: make(chan struct{})}
	if byAccount == nil {
		byAccount = make(map[*RealtimeClient]struct{})
		h.clients[accountID] = byAccount
	}
	byAccount[client] = struct{}{}
	h.connections++
	return client, len(byAccount) == 1, nil
}

func (h *RealtimeHub) Unregister(client *RealtimeClient) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wasOnline := len(h.clients[client.accountID]) > 0
	h.removeLocked(client)
	return wasOnline && len(h.clients[client.accountID]) == 0
}

func (h *RealtimeHub) Publish(accounts []string, event RealtimeEvent) {
	h.PublishExcept(accounts, "", event)
}

func (h *RealtimeHub) PublishExcept(accounts []string, excludedAccount string, event RealtimeEvent) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := make(map[string]struct{}, len(accounts))
	for _, accountID := range accounts {
		if accountID == excludedAccount {
			continue
		}
		if _, duplicate := seen[accountID]; duplicate {
			continue
		}
		seen[accountID] = struct{}{}
		for client := range h.clients[accountID] {
			select {
			case client.queue <- encoded:
			default:
				// A slow consumer must reconnect and recover from durable REST history.
				h.removeLocked(client)
			}
		}
	}
}

func (h *RealtimeHub) removeLocked(client *RealtimeClient) {
	byAccount := h.clients[client.accountID]
	if _, exists := byAccount[client]; !exists {
		return
	}
	delete(byAccount, client)
	for key, entry := range h.presence {
		delete(entry.subscribers, client)
		if len(entry.subscribers) == 0 {
			delete(h.presence, key)
		}
	}
	if len(byAccount) == 0 {
		delete(h.clients, client.accountID)
	}
	h.connections--
	close(client.done)
}

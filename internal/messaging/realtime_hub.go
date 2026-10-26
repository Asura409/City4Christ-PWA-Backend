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
	MessageID      string `json:"messageId"`
	Sequence       int64  `json:"sequence,omitempty"`
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
		queueCapacity:       queueCapacity,
		maxPerAccount:       maxPerAccount,
		maxTotalConnections: maxTotalConnections,
	}
}

func (h *RealtimeHub) Register(accountID string) (*RealtimeClient, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	byAccount := h.clients[accountID]
	if len(byAccount) >= h.maxPerAccount || h.connections >= h.maxTotalConnections {
		return nil, ErrRealtimeCapacity
	}
	client := &RealtimeClient{accountID: accountID, queue: make(chan []byte, h.queueCapacity), done: make(chan struct{})}
	if byAccount == nil {
		byAccount = make(map[*RealtimeClient]struct{})
		h.clients[accountID] = byAccount
	}
	byAccount[client] = struct{}{}
	h.connections++
	return client, nil
}

func (h *RealtimeHub) Unregister(client *RealtimeClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(client)
}

func (h *RealtimeHub) Publish(accounts []string, event RealtimeEvent) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := make(map[string]struct{}, len(accounts))
	for _, accountID := range accounts {
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
	if len(byAccount) == 0 {
		delete(h.clients, client.accountID)
	}
	h.connections--
	close(client.done)
}

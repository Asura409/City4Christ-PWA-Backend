package messaging

import "encoding/json"

const maxPresenceSubscriptionsPerClient = 20

type RealtimePresenceTarget struct {
	Scope   string
	ID      string
	Members []string
}

type realtimePresenceEntry struct {
	target      RealtimePresenceTarget
	subscribers map[*RealtimeClient]struct{}
}

func presenceKey(scope, id string) string { return scope + ":" + id }

func (h *RealtimeHub) IsOnline(accountID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients[accountID]) != 0
}

// SubscribePresence registers a verified chat-membership snapshot and returns a
// point-in-time status for other active members. Presence is never persisted.
func (h *RealtimeHub) SubscribePresence(client *RealtimeClient, target RealtimePresenceTarget) bool {
	if target.ID == "" || (target.Scope != "direct" && target.Scope != "group") {
		return false
	}
	containsClient := false
	seen := make(map[string]struct{}, len(target.Members))
	for _, member := range target.Members {
		if member == client.accountID {
			containsClient = true
		}
		if _, exists := seen[member]; exists {
			return false
		}
		seen[member] = struct{}{}
	}
	if !containsClient {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, registered := h.clients[client.accountID][client]; !registered {
		return false
	}
	key := presenceKey(target.Scope, target.ID)
	entry := h.presence[key]
	if entry == nil {
		entry = &realtimePresenceEntry{subscribers: make(map[*RealtimeClient]struct{})}
		h.presence[key] = entry
	}
	if _, alreadySubscribed := entry.subscribers[client]; !alreadySubscribed && len(clientPresenceTargetsLocked(h, client)) >= maxPresenceSubscriptionsPerClient {
		return false
	}
	entry.target = clonePresenceTarget(target)
	entry.subscribers[client] = struct{}{}
	if target.Scope == "direct" {
		for _, member := range target.Members {
			if member == client.accountID {
				continue
			}
			online := len(h.clients[member]) > 0
			h.enqueueLocked(client, RealtimeEvent{Type: "presence.snapshot", Scope: target.Scope, ConversationID: target.ID, SenderID: member, Online: &online})
		}
	} else {
		count := 0
		for _, member := range target.Members {
			if member != client.accountID && len(h.clients[member]) > 0 {
				count++
			}
		}
		h.enqueueLocked(client, RealtimeEvent{Type: "presence.snapshot", Scope: target.Scope, GroupID: target.ID, OnlineCount: &count})
	}
	return true
}

func clientPresenceTargetsLocked(h *RealtimeHub, client *RealtimeClient) []string {
	keys := make([]string, 0, len(h.presence))
	for key, entry := range h.presence {
		if _, found := entry.subscribers[client]; found {
			keys = append(keys, key)
		}
	}
	return keys
}

func (h *RealtimeHub) UnsubscribePresence(client *RealtimeClient, scope, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := presenceKey(scope, id)
	if entry := h.presence[key]; entry != nil {
		delete(entry.subscribers, client)
		if len(entry.subscribers) == 0 {
			delete(h.presence, key)
		}
	}
}

func (h *RealtimeHub) PresenceTargetsForAccount(accountID string) []RealtimePresenceTarget {
	h.mu.Lock()
	defer h.mu.Unlock()
	targets := make([]RealtimePresenceTarget, 0)
	for _, entry := range h.presence {
		for _, member := range entry.target.Members {
			if member == accountID {
				targets = append(targets, clonePresenceTarget(entry.target))
				break
			}
		}
	}
	return targets
}

// PublishPresence updates only currently subscribed sockets whose accounts are
// in the latest authorization result supplied by the caller.
func (h *RealtimeHub) PublishPresence(target RealtimePresenceTarget, eligibleMembers []string, accountID string, online bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := presenceKey(target.Scope, target.ID)
	entry := h.presence[key]
	if entry == nil || !containsAccount(eligibleMembers, accountID) {
		return
	}
	entry.target = clonePresenceTarget(RealtimePresenceTarget{Scope: target.Scope, ID: target.ID, Members: eligibleMembers})
	event := RealtimeEvent{Type: "presence.offline", Scope: target.Scope, SenderID: accountID}
	if online {
		event.Type = "presence.online"
	}
	if target.Scope == "direct" {
		event.ConversationID = target.ID
		value := online
		event.Online = &value
	} else {
		event.GroupID = target.ID
		count := 0
		for _, member := range eligibleMembers {
			if member != accountID && len(h.clients[member]) > 0 {
				count++
			}
		}
		event.OnlineCount = &count
	}
	for client := range entry.subscribers {
		if client.accountID == accountID || !containsAccount(eligibleMembers, client.accountID) {
			continue
		}
		h.enqueueLocked(client, event)
	}
}

func (h *RealtimeHub) Send(client *RealtimeClient, event RealtimeEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, registered := h.clients[client.accountID][client]; registered {
		h.enqueueLocked(client, event)
	}
}

func (h *RealtimeHub) enqueueLocked(client *RealtimeClient, event RealtimeEvent) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	select {
	case client.queue <- encoded:
	default:
		h.removeLocked(client)
	}
}

func clonePresenceTarget(target RealtimePresenceTarget) RealtimePresenceTarget {
	return RealtimePresenceTarget{Scope: target.Scope, ID: target.ID, Members: append([]string(nil), target.Members...)}
}

func containsAccount(accounts []string, accountID string) bool {
	for _, account := range accounts {
		if account == accountID {
			return true
		}
	}
	return false
}

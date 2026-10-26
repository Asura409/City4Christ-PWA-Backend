package messaging

import (
	"encoding/json"
	"testing"
)

func TestRealtimeHubDisconnectsSlowConsumersAndKeepsFanoutBounded(t *testing.T) {
	hub := newRealtimeHub(1, 1, 2)
	fast, err := hub.Register("member-a")
	if err != nil {
		t.Fatal(err)
	}
	slow, err := hub.Register("member-b")
	if err != nil {
		t.Fatal(err)
	}
	event := RealtimeEvent{Type: "message.created", Scope: "direct", ConversationID: "conversation-id", MessageID: "message-id", Sequence: 7}
	hub.Publish([]string{"member-a", "member-b"}, event)
	first := <-fast.Events()
	var decoded RealtimeEvent
	if err := json.Unmarshal(first, &decoded); err != nil || decoded != event {
		t.Fatalf("event = (%s, %v), want %+v", first, err, event)
	}
	hub.Publish([]string{"member-b"}, event)
	select {
	case <-slow.Done():
	default:
		t.Fatal("slow consumer was not disconnected after its bounded queue filled")
	}
	hub.Publish([]string{"member-a"}, event)
	if got := len(fast.queue); got != 1 {
		t.Fatalf("fast queue length = %d, want 1", got)
	}
	if _, err := hub.Register("member-b"); err != nil {
		t.Fatalf("disconnected consumer could not reconnect: %v", err)
	}
}

func TestRealtimeHubEnforcesPerAccountAndGlobalConnectionLimits(t *testing.T) {
	hub := newRealtimeHub(2, 1, 1)
	if _, err := hub.Register("member-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Register("member-a"); err != ErrRealtimeCapacity {
		t.Fatalf("second connection for account err = %v", err)
	}
	if _, err := hub.Register("member-b"); err != ErrRealtimeCapacity {
		t.Fatalf("connection over global limit err = %v", err)
	}
}

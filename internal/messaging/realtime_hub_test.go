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

func TestRealtimeHubTracksPresenceOnlyForSubscribedChatsAndLastConnection(t *testing.T) {
	hub := newRealtimeHub(8, 2, 8)
	bob, _, err := hub.RegisterWithStatus("member-b")
	if err != nil {
		t.Fatal(err)
	}
	target := RealtimePresenceTarget{Scope: "direct", ID: "conversation-1", Members: []string{"member-a", "member-b"}}
	if !hub.SubscribePresence(bob, target) {
		t.Fatal("participant could not subscribe to presence")
	}
	var snapshot RealtimeEvent
	if err := json.Unmarshal(<-bob.Events(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "presence.snapshot" || snapshot.Online == nil || *snapshot.Online || snapshot.SenderID != "member-a" {
		t.Fatalf("initial presence = %+v", snapshot)
	}
	alice, first, err := hub.RegisterWithStatus("member-a")
	if err != nil || !first {
		t.Fatalf("first account connection = (%v, %v)", first, err)
	}
	hub.PublishPresence(target, target.Members, "member-a", true)
	var online RealtimeEvent
	if err := json.Unmarshal(<-bob.Events(), &online); err != nil {
		t.Fatal(err)
	}
	if online.Type != "presence.online" || online.SenderID != "member-a" {
		t.Fatalf("online event = %+v", online)
	}
	secondAlice, first, err := hub.RegisterWithStatus("member-a")
	if err != nil || first {
		t.Fatalf("second account connection = (%v, %v)", first, err)
	}
	if last := hub.Unregister(alice); last {
		t.Fatal("closing first of two connections marked account offline")
	}
	if !hub.IsOnline("member-a") {
		t.Fatal("account went offline while another connection remains")
	}
	if last := hub.Unregister(secondAlice); !last {
		t.Fatal("closing final connection did not mark account offline")
	}
	hub.PublishPresence(target, target.Members, "member-a", false)
	var offline RealtimeEvent
	if err := json.Unmarshal(<-bob.Events(), &offline); err != nil {
		t.Fatal(err)
	}
	if offline.Type != "presence.offline" || offline.SenderID != "member-a" {
		t.Fatalf("offline event = %+v", offline)
	}
	hub.UnsubscribePresence(bob, target.Scope, target.ID)
}

func TestRealtimeHubPresenceCountsOtherAuthorizedGroupMembers(t *testing.T) {
	hub := newRealtimeHub(8, 2, 8)
	memberA, _, err := hub.RegisterWithStatus("member-a")
	if err != nil {
		t.Fatal(err)
	}
	memberC, _, err := hub.RegisterWithStatus("member-c")
	if err != nil {
		t.Fatal(err)
	}
	target := RealtimePresenceTarget{Scope: "group", ID: "group-1", Members: []string{"member-a", "member-b", "member-c"}}
	if !hub.SubscribePresence(memberC, target) {
		t.Fatal("current group member could not subscribe")
	}
	var initial RealtimeEvent
	if err := json.Unmarshal(<-memberC.Events(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Type != "presence.snapshot" || initial.OnlineCount == nil || *initial.OnlineCount != 1 {
		t.Fatalf("group snapshot = %+v", initial)
	}
	memberB, first, err := hub.RegisterWithStatus("member-b")
	if err != nil || !first {
		t.Fatalf("new member first connection = %v, %v", first, err)
	}
	hub.PublishPresence(target, target.Members, "member-b", true)
	var online RealtimeEvent
	if err := json.Unmarshal(<-memberC.Events(), &online); err != nil {
		t.Fatal(err)
	}
	if online.Type != "presence.online" || online.SenderID != "member-b" || online.OnlineCount == nil || *online.OnlineCount != 2 {
		t.Fatalf("group online = %+v", online)
	}
	hub.Unregister(memberA)
	hub.Unregister(memberB)
	hub.Unregister(memberC)
}

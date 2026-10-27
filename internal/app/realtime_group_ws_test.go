package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestRealtimeGroupMessageDeliveredOnlyToCurrentMembers(t *testing.T) {
	db, sessions, senderID, senderAccess := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	account := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Group','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Start(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return id, tokens.AccessToken
	}

	activeID, activeAccess := account("realtime-group-active@example.invalid")
	formerID, formerAccess := account("realtime-group-former@example.invalid")
	_, outsiderAccess := account("realtime-group-outsider@example.invalid")
	groupID := "44444444-4444-4444-8444-444444444444"
	if _, err := db.ExecContext(ctx, `INSERT INTO groups(id,name,description,audience,join_policy,created_by) VALUES($1,'Choir','','church','open',$2)`, groupID, senderID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active'),($1,$3,'active'),($1,$4,'left')`, groupID, senderID, activeID, formerID); err != nil {
		t.Fatal(err)
	}

	origin := "https://pwa.example.invalid"
	server := httptest.NewServer(New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{origin}}))
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
			t.Fatalf("socket ready = %v, err = %v", ready, err)
		}
		return conn
	}
	activeSocket := open(activeAccess)
	formerSocket := open(formerAccess)
	outsiderSocket := open(outsiderAccess)
	defer activeSocket.CloseNow()
	defer formerSocket.CloseNow()
	defer outsiderSocket.CloseNow()

	payload := `{"clientMessageId":"35b3da77-4211-46e8-84b7-dac881208bab","body":"This text stays in REST history."}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/groups/"+groupID+"/messages", strings.NewReader(payload))
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
		t.Fatalf("group send status = %d; want 201", response.StatusCode)
	}
	var created struct {
		Data struct {
			ID       string `json:"id"`
			Sequence int64  `json:"sequence"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	var event map[string]any
	if err := wsjson.Read(ctx, activeSocket, &event); err != nil {
		t.Fatalf("active member did not receive group event: %v", err)
	}
	if event["type"] != "message.created" || event["scope"] != "group" || event["groupId"] != groupID || event["messageId"] != created.Data.ID || event["sequence"] != float64(created.Data.Sequence) {
		t.Fatalf("unexpected active-member event: %#v", event)
	}
	if _, hasBody := event["body"]; hasBody {
		t.Fatalf("group event exposed message content: %#v", event)
	}

	for label, conn := range map[string]*websocket.Conn{"former member": formerSocket, "nonmember": outsiderSocket} {
		readCtx, readCancel := context.WithTimeout(ctx, 250*time.Millisecond)
		var unexpected any
		err := wsjson.Read(readCtx, conn, &unexpected)
		readCancel()
		if err == nil {
			t.Fatalf("%s received a group event: %#v", label, unexpected)
		}
	}

}

func TestRealtimeWebSocketClosesAfterLogoutOrSessionExpiry(t *testing.T) {
	db, sessions, logoutAccountID, _ := realtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logoutTokens, err := sessions.Start(ctx, logoutAccountID)
	if err != nil {
		t.Fatal(err)
	}
	var expiredAccountID string
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('realtime-expired@example.invalid','Expired','Session',now()) RETURNING id`).Scan(&expiredAccountID); err != nil {
		t.Fatal(err)
	}
	expiredTokens, err := sessions.Start(ctx, expiredAccountID)
	if err != nil {
		t.Fatal(err)
	}
	_, expiredSessionID, _, err := sessions.AuthenticateSession(ctx, expiredTokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}

	origin := "https://pwa.example.invalid"
	server := httptest.NewServer(New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{origin}}))
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
			t.Fatalf("socket ready = %v, err = %v", ready, err)
		}
		return conn
	}
	logoutSocket := open(logoutTokens.AccessToken)
	expiredSocket := open(expiredTokens.AccessToken)
	defer logoutSocket.CloseNow()
	defer expiredSocket.CloseNow()

	h := New(db, Config{VerificationKey: bytes.Repeat([]byte{73}, 32), AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{origin}})
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.AddCookie(&http.Cookie{Name: refreshCookieName, Value: logoutTokens.RefreshToken})
	logoutRequest.Header.Set("Origin", origin)
	logoutResponse := httptest.NewRecorder()
	h.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout status = %d %s; want 200", logoutResponse.Code, logoutResponse.Body.String())
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, expiredSessionID); err != nil {
		t.Fatal(err)
	}

	for label, conn := range map[string]*websocket.Conn{"logged-out session": logoutSocket, "expired session": expiredSocket} {
		readCtx, readCancel := context.WithTimeout(ctx, 17*time.Second)
		var unexpected any
		err := wsjson.Read(readCtx, conn, &unexpected)
		readCancel()
		if err == nil {
			t.Fatalf("%s received a frame after session invalidation: %#v", label, unexpected)
		}
	}
}

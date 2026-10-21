//go:build frontend_e2e

package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/pquerna/otp/totp"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

// Synthetic provider used only by the loopback/disposable browser harness.
// Production signature verification is covered independently by googleoidc tests.
type browserReauthenticationProvider struct{ proofs sync.Map }

func (p *browserReauthenticationProvider) AuthURL(state, nonce, verifier string) string {
	p.proofs.Store(nonce, verifier)
	return "https://accounts.google.com/auth?" + url.Values{"state": {state}, "nonce": {nonce}}.Encode()
}
func (p *browserReauthenticationProvider) Exchange(_ context.Context, code, nonce, verifier string) (auth.GoogleIdentity, error) {
	expected, ok := p.proofs.LoadAndDelete(nonce)
	if !ok || expected != verifier || !strings.HasPrefix(code, "e2e-google-") {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	return auth.GoogleIdentity{Subject: code, Email: code + "@example.invalid", EmailVerified: true, AuthenticatedAt: time.Now().UTC()}, nil
}
func browserReauthenticationFixture(w http.ResponseWriter, r *http.Request, db *sql.DB, sessions *auth.SessionService, c Config) bool {
	switch r.URL.Path {
	case "/__test/mfa-code":
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return true
		}
		var input struct{ Secret string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
			http.Error(w, "fixture input", 400)
			return true
		}
		code, err := totp.GenerateCode(input.Secret, time.Now().UTC())
		if err != nil {
			http.Error(w, "fixture input", 400)
			return true
		}
		writeJSON(w, 200, map[string]string{"code": code})
		return true
	case "/__test/session/age":
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return true
		}
		_, err := db.ExecContext(r.Context(), `UPDATE auth_sessions SET authenticated_at=now()-interval '6 minutes',reauthenticated_at=NULL,reauthentication_method=NULL WHERE account_id=(SELECT id FROM accounts WHERE email=$1)`, r.URL.Query().Get("email"))
		if err != nil {
			http.Error(w, "fixture failed", 500)
			return true
		}
		w.WriteHeader(204)
		return true
	case "/__test/google-account":
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return true
		}
		subject := r.URL.Query().Get("subject")
		if !strings.HasPrefix(subject, "e2e-google-") {
			http.Error(w, "synthetic identity required", 400)
			return true
		}
		var id string
		err := db.QueryRowContext(r.Context(), `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Google','Browser',now()) RETURNING id`, subject+"@example.invalid").Scan(&id)
		if err != nil {
			http.Error(w, "fixture failed", 500)
			return true
		}
		if _, err = db.ExecContext(r.Context(), `INSERT INTO external_identities(provider,subject,account_id) VALUES('google',$1,$2)`, subject, id); err != nil {
			http.Error(w, "fixture failed", 500)
			return true
		}
		tokens, err := sessions.Start(r.Context(), id)
		if err != nil {
			http.Error(w, "fixture failed", 500)
			return true
		}
		setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
		w.WriteHeader(204)
		return true
	}
	return false
}

// Distinct browser scenarios must not exhaust one shared loopback IP's quota.
// This header is interpreted only by this build-tagged test harness.
type browserScenarioIPs struct {
	mu       sync.Mutex
	assigned map[string]string
}

func (b *browserScenarioIPs) apply(r *http.Request) {
	key := r.Header.Get("X-C4C-Test-Scenario")
	if key == "" || len(key) > 256 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.assigned == nil {
		b.assigned = map[string]string{}
	}
	ip, ok := b.assigned[key]
	if !ok {
		if len(b.assigned) >= 250 {
			return
		}
		ip = fmt.Sprintf("192.0.2.%d:1234", len(b.assigned)+1)
		b.assigned[key] = ip
	}
	r.RemoteAddr = ip
}

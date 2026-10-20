//go:build frontend_e2e

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

// This optional suite serves the independent frontend checkout against the global
// app. testpg starts its own cluster and never reads a staging DATABASE_URL.
func TestFrontendWithRealAPI(t *testing.T) {
	dist, err := filepath.Abs("../../frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dist, "auth.html")); err != nil {
		t.Fatal("Build frontend/dist before running frontend_e2e: ", err)
	}
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("t", 32))
	verification, err := auth.NewVerificationService(db, auth.VerificationConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	api := New(db, Config{RegistrationEnabled: true, VerificationKey: key, AppOrigin: origin})
	files := http.FileServer(http.Dir(dist))
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/js/config.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte("globalThis.City4ChristConfig={API_URL:'/'};"))
		case r.URL.Path == "/__test/inbox":
			// Exists only in this build-tagged, loopback test server. No email is sent.
			var account, challenge string
			var sealed []byte
			err := db.QueryRowContext(r.Context(), `SELECT a.id,o.challenge_id,o.sealed_payload FROM accounts a JOIN verification_email_outbox o ON o.account_id=a.id WHERE a.email=$1`, r.URL.Query().Get("email")).Scan(&account, &challenge, &sealed)
			if err != nil {
				http.Error(w, "test inbox unavailable", 404)
				return
			}
			message, err := verification.OpenEmail(account, challenge, sealed)
			if err != nil {
				http.Error(w, "test inbox unavailable", 500)
				return
			}
			writeJSON(w, 200, map[string]string{"code": message.Code, "id": account})
		case r.URL.Path == "/__test/recovery-inbox":
			// Recovery secrets are exposed only inside this disposable test harness.
			var account, id string
			var sealed []byte
			err := db.QueryRowContext(r.Context(), `SELECT a.id,o.id,o.sealed_payload FROM accounts a JOIN password_recovery_outbox o ON o.account_id=a.id WHERE a.email=$1 AND o.kind='reset'`, r.URL.Query().Get("email")).Scan(&account, &id, &sealed)
			if err != nil {
				http.Error(w, "test inbox unavailable", 404)
				return
			}
			recovery := auth.NewPasswordRecoveryService(db, verification)
			message, err := recovery.OpenEmail(account, id, sealed)
			if err != nil {
				http.Error(w, "test inbox unavailable", 500)
				return
			}
			writeJSON(w, 200, map[string]string{"resetURL": message.ResetURL})
		case r.URL.Path == "/__test/editor/revoke" && r.Method == "POST":
			// Isolated test operator fixture, never part of the production router.
			_, err := db.ExecContext(r.Context(), `DELETE FROM account_roles WHERE account_id=(SELECT id FROM accounts WHERE email=$1) AND role='content_editor' AND scope=''`, r.URL.Query().Get("email"))
			if err != nil {
				http.Error(w, "test fixture failed", 500)
				return
			}
			w.WriteHeader(204)
		case r.URL.Path == "/__test/editor" && r.Method == "POST":
			// An operator fixture, never a production HTTP role-grant route.
			_, err := db.ExecContext(r.Context(), `INSERT INTO account_roles(account_id,role,scope) SELECT id,'content_editor','' FROM accounts WHERE email=$1 AND email_verified_at IS NOT NULL ON CONFLICT DO NOTHING`, r.URL.Query().Get("email"))
			if err != nil {
				http.Error(w, "test fixture failed", 500)
				return
			}
			w.WriteHeader(204)
		case strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/health/"):
			api.ServeHTTP(w, r)
		default:
			files.ServeHTTP(w, r)
		}
	})
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "exec", "--yes", "--package=node@22", "--", "node", "node_modules/playwright/cli.js", "test", "tests/browser/real-api.spec.mjs")
	cmd.Dir = filepath.Dir(dist)
	cmd.Env = append(os.Environ(), "E2E_BASE_URL="+origin)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}

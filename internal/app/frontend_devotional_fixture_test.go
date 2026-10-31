//go:build frontend_e2e

package app

import (
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/google/uuid"
	"net/http"
)

// Only the disposable loopback browser server includes this synthetic session fixture.
func browserDevotionalFixture(w http.ResponseWriter, r *http.Request, db *sql.DB, sessions *auth.SessionService, c Config) bool {
	if r.URL.Path != "/__test/devotional-session" {
		return false
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return true
	}
	scenario, err := uuid.Parse(r.URL.Query().Get("scenario"))
	role := r.URL.Query().Get("role")
	if err != nil || (role != "editor" && role != "member") {
		http.Error(w, "invalid fixture", 400)
		return true
	}
	var id string
	err = db.QueryRowContext(r.Context(), `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Devotional',$2,now()) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id`, "devotional-"+scenario.String()+"-"+role+"@example.invalid", role).Scan(&id)
	if err == nil && role == "editor" {
		_, err = db.ExecContext(r.Context(), `INSERT INTO account_roles(account_id,role,scope) VALUES($1,'content_editor','') ON CONFLICT DO NOTHING`, id)
	}
	if err != nil {
		http.Error(w, "fixture account failed", 500)
		return true
	}
	tokens, err := sessions.Start(r.Context(), id)
	if err != nil {
		http.Error(w, "fixture session failed", 500)
		return true
	}
	setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
	writeJSON(w, 200, map[string]any{"data": map[string]string{"accountId": id}})
	return true
}

//go:build frontend_e2e

package app

import (
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/google/uuid"
	"net/http"
)

func browserVisitorFixture(w http.ResponseWriter, r *http.Request, db *sql.DB, sessions *auth.SessionService, c Config) bool {
	if r.URL.Path != "/__test/visitor-staff-session" {
		return false
	}
	id, e := uuid.Parse(r.URL.Query().Get("scenario"))
	if r.Method != "POST" || e != nil {
		http.Error(w, "invalid fixture", 400)
		return true
	}
	var account string
	e = db.QueryRowContext(r.Context(), `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Synthetic','Welcome Staff',now()) RETURNING id`, "visitor-staff-"+id.String()+"@example.invalid").Scan(&account)
	if e == nil {
		_, e = db.ExecContext(r.Context(), `INSERT INTO account_roles(account_id,role,scope) VALUES($1,'staff','')`, account)
	}
	if e != nil {
		http.Error(w, "fixture account failed", 500)
		return true
	}
	tokens, e := sessions.Start(r.Context(), account)
	if e != nil {
		http.Error(w, "fixture session failed", 500)
		return true
	}
	setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
	writeJSON(w, 200, map[string]bool{"ready": true})
	return true
}

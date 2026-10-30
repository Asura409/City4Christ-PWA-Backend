//go:build frontend_e2e

package app

import (
	"context"
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"github.com/google/uuid"
	"net/http"
	"time"
)

// This fixture exists only in the loopback, disposable-PostgreSQL browser harness.
func browserPastoralFixture(w http.ResponseWriter, r *http.Request, db *sql.DB, sessions *auth.SessionService, c Config) bool {
	if r.URL.Path != "/__test/pastoral-session" {
		return false
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return true
	}
	scenario, err := uuid.Parse(r.URL.Query().Get("scenario"))
	role := r.URL.Query().Get("role")
	if err != nil || (role != "pastor" && role != "member") {
		http.Error(w, "invalid test fixture", 400)
		return true
	}
	accounts := map[string]string{}
	for _, kind := range []string{"admin", "pastor", "member"} {
		email := "pastoral-" + scenario.String() + "-" + kind + "@example.invalid"
		last := "Member"
		if kind == "pastor" {
			last = "Pastor " + scenario.String()[:8]
		}
		var id string
		err = db.QueryRowContext(r.Context(), `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Care',$2,now()) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id`, email, last).Scan(&id)
		if err != nil {
			http.Error(w, "fixture account failed", 500)
			return true
		}
		accounts[kind] = id
	}
	for _, entry := range []struct{ kind, role string }{{"admin", "super_administrator"}, {"pastor", "assistant_pastor"}} {
		if _, err = db.ExecContext(r.Context(), `INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,'') ON CONFLICT DO NOTHING`, accounts[entry.kind], entry.role); err != nil {
			http.Error(w, "fixture role failed", 500)
			return true
		}
	}
	service := pastoral.NewService(pastoral.NewRepository(db), time.Now)
	if _, err = service.Appoint(r.Context(), accounts["admin"], pastoral.ProfileInput{AccountID: accounts["pastor"], TimeZone: "UTC", Bio: "Synthetic appointment fixture"}, func(context.Context, *sql.Tx) error { return nil }); err != nil {
		http.Error(w, "fixture appointment failed", 500)
		return true
	}
	tokens, err := sessions.Start(r.Context(), accounts[role])
	if err != nil {
		http.Error(w, "fixture session failed", 500)
		return true
	}
	setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
	writeJSON(w, 200, map[string]any{"data": map[string]string{"accountId": accounts[role], "pastorId": accounts["pastor"], "pastorName": "Care Pastor " + scenario.String()[:8]}})
	return true
}

//go:build frontend_e2e

package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
)

// browserChurchRoleFixture exists only in the loopback, disposable PostgreSQL
// harness. Production binaries have no bootstrap HTTP endpoint.
func browserChurchRoleFixture(w http.ResponseWriter, r *http.Request, db *sql.DB) bool {
	if r.URL.Path != "/__test/super-admin" {
		return false
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return true
	}
	var id string
	err := db.QueryRowContext(r.Context(), `SELECT a.id FROM accounts a JOIN mfa_factors m ON m.account_id=a.id AND m.confirmed_at IS NOT NULL WHERE a.email=$1 AND a.email_verified_at IS NOT NULL`, r.URL.Query().Get("email")).Scan(&id)
	if err != nil {
		http.Error(w, "verified MFA account required", 422)
		return true
	}
	err = access.NewService(db).BootstrapChurchAdmin(r.Context(), id, "Disposable browser test operator fixture")
	if errors.Is(err, access.ErrBootstrapConflict) {
		// Desktop/mobile cases share the disposable cluster. The first case rehearses
		// real one-time bootstrap; subsequent scenarios explicitly seed their own test
		// operator, exactly as the existing build-tagged editor fixture does.
		_, err = db.ExecContext(r.Context(), `INSERT INTO account_roles(account_id,role) VALUES($1,'super_administrator') ON CONFLICT DO NOTHING`, id)
	}
	if err != nil {
		http.Error(w, "test operator fixture failed", 500)
		return true
	}
	w.WriteHeader(204)
	return true
}

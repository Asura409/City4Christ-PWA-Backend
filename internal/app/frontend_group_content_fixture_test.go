//go:build frontend_e2e

package app

import (
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

// This fixture exists only in the build-tagged disposable loopback test server.
func browserGroupContentFixture(w http.ResponseWriter, r *http.Request, db *sql.DB, sessions *auth.SessionService, c Config) bool {
	if r.URL.Path != "/__test/group-session" {
		return false
	}
	scenario, err := uuid.Parse(r.URL.Query().Get("scenario"))
	role := r.URL.Query().Get("role")
	if r.Method != "POST" || err != nil || (role != "member" && role != "leader" && role != "outsider") {
		http.Error(w, "invalid fixture", 400)
		return true
	}
	ids := map[string]string{}
	for _, name := range []string{"leader", "member", "outsider"} {
		ids[name] = uuid.NewSHA1(scenario, []byte(name)).String()
		_, err = db.ExecContext(r.Context(), `INSERT INTO accounts(id,email,first_name,last_name,email_verified_at) VALUES($1,$2,'Group',$3,now()) ON CONFLICT DO NOTHING`, ids[name], scenario.String()+name+"@example.invalid", name)
		if err != nil {
			http.Error(w, "account fixture failed", 500)
			return true
		}
	}
	group := uuid.NewSHA1(scenario, []byte("group")).String()
	for _, q := range []string{`INSERT INTO groups(id,name,description,audience,join_policy,created_by) VALUES($1,'Synthetic choir','','church','open',$2) ON CONFLICT DO NOTHING`, `INSERT INTO access_scopes(id,kind) VALUES($1,'ministry') ON CONFLICT DO NOTHING`, `INSERT INTO account_roles(account_id,role,scope) VALUES($2,'ministry_leader',$1) ON CONFLICT DO NOTHING`, `INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active') ON CONFLICT DO NOTHING`} {
		args := []any{group}
		if strings.Contains(q, "$2") {
			args = append(args, ids["leader"])
		}
		_, err = db.ExecContext(r.Context(), q, args...)
		if err != nil {
			http.Error(w, "group fixture failed", 500)
			return true
		}
	}
	if _, err = db.ExecContext(r.Context(), `INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active') ON CONFLICT DO NOTHING`, group, ids["member"]); err != nil {
		http.Error(w, "membership fixture failed", 500)
		return true
	}
	tokens, err := sessions.Start(r.Context(), ids[role])
	if err != nil {
		http.Error(w, "session fixture failed", 500)
		return true
	}
	setRefresh(w, c, tokens.RefreshToken, tokens.RefreshExpiresAt)
	writeJSON(w, 200, map[string]any{"data": map[string]string{"groupId": group, "accountId": ids[role]}})
	return true
}

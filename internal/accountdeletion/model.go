// Package accountdeletion records reviewed deletion requests without performing a
// permanent purge or removing shared church records.
package accountdeletion

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

const PolicyVersion = "account-deletion-request-v1"
const Notice = "Requesting account deletion immediately disables sign-in, signs you out on all devices and hides your directory profile. Church administrators will review the request. Shared posts, messages and audit records are retained; this request does not permanently erase your data."

var ErrInvalid = errors.New("invalid account deletion request")
var ErrNotFound = errors.New("account deletion request not found")
var ErrConflict = errors.New("account deletion request version conflict")
var ErrActiveCommitments = errors.New("active pastoral appointments must be resolved first")

type Guard func(context.Context, *sql.Tx) error

type Request struct {
	ID            string    `json:"id"`
	AccountID     string    `json:"accountId"`
	Status        string    `json:"status"`
	Version       int       `json:"version"`
	PolicyVersion string    `json:"policyVersion"`
	Consent       bool      `json:"consent"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
type Status struct {
	Requested     bool     `json:"requested"`
	Request       *Request `json:"request,omitempty"`
	PolicyVersion string   `json:"policyVersion"`
	Notice        string   `json:"notice"`
}
type Summary struct {
	Request
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}
type Detail struct {
	Summary
	Email string `json:"email"`
}
type Page struct {
	Items      []Summary `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}
type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(db *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}
func validID(s string) bool {
	u, e := uuid.Parse(s)
	return e == nil && u != uuid.Nil && u.String() == s
}

const columns = `d.id,d.account_id,d.status,d.version,d.policy_version,d.consent,d.created_at,d.updated_at`

func scanRequest(row interface{ Scan(...any) error }) (Request, error) {
	var r Request
	e := row.Scan(&r.ID, &r.AccountID, &r.Status, &r.Version, &r.PolicyVersion, &r.Consent, &r.CreatedAt, &r.UpdatedAt)
	return r, e
}
func (s *Service) audit(ctx context.Context, tx *sql.Tx, actor string, r Request, action string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO account_deletion_audit_events(request_id,actor_id,action,status,version,occurred_at)VALUES($1,$2,$3,$4,$5,$6)`, r.ID, actor, action, r.Status, r.Version, s.now().UTC())
	return e
}

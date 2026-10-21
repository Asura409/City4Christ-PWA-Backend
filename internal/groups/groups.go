// Package groups implements scoped church groups and membership workflows.
package groups

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/google/uuid"
)

var (
	ErrForbidden = errors.New("group action forbidden")
	ErrNotFound  = errors.New("group not found")
	ErrInvalid   = errors.New("invalid group input")
	ErrConflict  = errors.New("group conflicts with existing church configuration")
)

type Group struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Audience         string    `json:"audience"`
	JoinPolicy       string    `json:"joinPolicy"`
	MembershipStatus string    `json:"membershipStatus,omitempty"`
	MemberCount      int       `json:"memberCount"`
	CreatedAt        time.Time `json:"createdAt"`
}
type Membership struct {
	GroupID     string    `json:"groupId"`
	AccountID   string    `json:"accountId"`
	Status      string    `json:"status"`
	RequestedAt time.Time `json:"requestedAt"`
}
type CreateInput struct{ Name, Description, Audience, JoinPolicy string }
type DecisionInput struct{ Decision, Reason string }
type Options struct {
	Limit  int
	Cursor string
}
type Page struct {
	Items      []Group `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}
type JoinRequest struct {
	AccountID   string    `json:"accountId"`
	FirstName   string    `json:"firstName"`
	LastName    string    `json:"lastName"`
	RequestedAt time.Time `json:"requestedAt"`
}
type Repository struct{ db *sql.DB }
type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func NewService(repo *Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: repo.db, now: now}
}
func validID(s string) bool {
	id, err := uuid.Parse(s)
	return err == nil && id.String() == s && id != uuid.Nil
}
func leadershipEligible(ctx context.Context, tx *sql.Tx, actor string) (bool, error) {
	var eligible bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=$1 AND ((role IN ('super_administrator','assistant_pastor') AND scope='') OR role='ministry_leader'))`, actor).Scan(&eligible)
	return eligible, err
}
func (s *Service) Create(ctx context.Context, actor string, in CreateInput) (Group, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if !validID(actor) || !utf8.ValidString(in.Name) || len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 120 || len(in.Description) > 2000 || !utf8.ValidString(in.Description) || strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.Description, 0) || (in.Audience != "church" && in.Audience != "leadership") || (in.JoinPolicy != "open" && in.JoinPolicy != "approval" && in.JoinPolicy != "closed") {
		return Group{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback()
	if err = access.CheckTx(ctx, tx, actor, "groups.create", ""); err != nil {
		if errors.Is(err, access.ErrForbidden) {
			return Group{}, ErrForbidden
		}
		return Group{}, err
	}
	if in.Audience == "leadership" {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('city4christ:single-leadership-group'))`); err != nil {
			return Group{}, err
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE audience='leadership' AND archived_at IS NULL)`).Scan(&exists); err != nil {
			return Group{}, err
		}
		if exists {
			return Group{}, ErrConflict
		}
	}
	id := uuid.NewString()
	var g Group
	err = tx.QueryRowContext(ctx, `INSERT INTO groups(id,name,description,audience,join_policy,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,name,description,audience,join_policy,created_at`, id, in.Name, in.Description, in.Audience, in.JoinPolicy, actor, s.now()).Scan(&g.ID, &g.Name, &g.Description, &g.Audience, &g.JoinPolicy, &g.CreatedAt)
	if err != nil {
		return Group{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, id); err != nil {
		return Group{}, err
	}
	if err = tx.Commit(); err != nil {
		return Group{}, err
	}
	return g, nil
}
func (s *Service) List(ctx context.Context, actor string, opt Options) (Page, error) {
	if !validID(actor) || opt.Limit < 1 || opt.Limit > 100 || (opt.Cursor != "" && !validID(opt.Cursor)) {
		return Page{}, ErrInvalid
	}
	var verified bool
	if err := s.db.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1`, actor).Scan(&verified); errors.Is(err, sql.ErrNoRows) || (err == nil && !verified) {
		return Page{}, ErrForbidden
	} else if err != nil {
		return Page{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT g.id,g.name,g.description,g.audience,g.join_policy,COALESCE(m.status,''),count(a.account_id)::int,g.created_at FROM groups g LEFT JOIN group_memberships m ON m.group_id=g.id AND m.account_id=$1 LEFT JOIN group_memberships a ON a.group_id=g.id AND a.status='active' WHERE g.archived_at IS NULL AND (g.audience='church' OR (g.audience='leadership' AND EXISTS(SELECT 1 FROM account_roles r WHERE r.account_id=$1 AND ((r.role IN ('super_administrator','assistant_pastor') AND r.scope='') OR r.role='ministry_leader')))) AND ($2='' OR (g.created_at,g.id)>(SELECT created_at,id FROM groups WHERE id=$2::uuid)) GROUP BY g.id,m.status ORDER BY g.created_at,g.id LIMIT $3`, actor, opt.Cursor, opt.Limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	p := Page{Items: []Group{}}
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.ID, &g.Name, &g.Description, &g.Audience, &g.JoinPolicy, &g.MembershipStatus, &g.MemberCount, &g.CreatedAt); err != nil {
			return Page{}, err
		}
		p.Items = append(p.Items, g)
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	if len(p.Items) > opt.Limit {
		p.NextCursor = p.Items[opt.Limit-1].ID
		p.Items = p.Items[:opt.Limit]
	}
	return p, nil
}
func (s *Service) Join(ctx context.Context, actor, groupID string) (Membership, error) {
	if !validID(actor) || !validID(groupID) {
		return Membership{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Membership{}, err
	}
	defer tx.Rollback()
	var audience, policy string
	err = tx.QueryRowContext(ctx, `SELECT audience,join_policy FROM groups WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, groupID).Scan(&audience, &policy)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, err
	}
	var verified bool
	err = tx.QueryRowContext(ctx, `SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1`, actor).Scan(&verified)
	if errors.Is(err, sql.ErrNoRows) || !verified {
		return Membership{}, ErrForbidden
	}
	if err != nil {
		return Membership{}, err
	}
	if audience == "leadership" {
		eligible, e := leadershipEligible(ctx, tx, actor)
		err = e
		if err != nil {
			return Membership{}, err
		}
		if !eligible {
			return Membership{}, ErrNotFound
		}
	}
	if policy == "closed" {
		return Membership{}, ErrForbidden
	}
	status := "pending"
	action := "requested"
	if policy == "open" {
		status = "active"
		action = "joined"
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT status FROM group_memberships WHERE group_id=$1 AND account_id=$2 FOR UPDATE`, groupID, actor).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Membership{}, err
	}
	var m Membership
	err = tx.QueryRowContext(ctx, `INSERT INTO group_memberships(group_id,account_id,status,requested_at) VALUES($1,$2,$3,$4) ON CONFLICT(group_id,account_id) DO UPDATE SET status=CASE WHEN group_memberships.status IN ('left','declined') THEN EXCLUDED.status ELSE group_memberships.status END,requested_at=CASE WHEN group_memberships.status IN ('left','declined') THEN EXCLUDED.requested_at ELSE group_memberships.requested_at END,decided_at=CASE WHEN group_memberships.status IN ('left','declined') THEN NULL ELSE group_memberships.decided_at END,decided_by=CASE WHEN group_memberships.status IN ('left','declined') THEN NULL ELSE group_memberships.decided_by END RETURNING group_id,account_id,status,requested_at`, groupID, actor, status, s.now()).Scan(&m.GroupID, &m.AccountID, &m.Status, &m.RequestedAt)
	if err != nil {
		return Membership{}, err
	}
	if m.Status == status && (previous == "" || previous == "left" || previous == "declined") {
		_, err = tx.ExecContext(ctx, `INSERT INTO group_audit_events(group_id,actor_id,target_id,action) VALUES($1,$2,$2,$3)`, groupID, actor, action)
		if err != nil {
			return Membership{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Membership{}, err
	}
	return m, nil
}
func (s *Service) Decide(ctx context.Context, actor, groupID, target string, in DecisionInput) (bool, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !validID(actor) || !validID(groupID) || !validID(target) || actor == target {
		return false, ErrForbidden
	}
	if (in.Decision != "approve" && in.Decision != "decline") || len(in.Reason) > 500 || !utf8.ValidString(in.Reason) || strings.ContainsRune(in.Reason, 0) {
		return false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var audience string
	err = tx.QueryRowContext(ctx, `SELECT audience FROM groups WHERE id=$1 AND archived_at IS NULL`, groupID).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if audience == "leadership" {
		eligible, e := leadershipEligible(ctx, tx, actor)
		if e != nil {
			return false, e
		}
		if !eligible {
			return false, ErrNotFound
		}
	}
	if audience == "" {
		return false, ErrNotFound
	}
	err = access.CheckTx(ctx, tx, actor, "groups.members.manage", "")
	if err != nil {
		if !errors.Is(err, access.ErrForbidden) {
			return false, err
		}
		err = access.CheckTx(ctx, tx, actor, "ministry.members.manage", groupID)
		if err != nil {
			if errors.Is(err, access.ErrForbidden) {
				return false, ErrForbidden
			}
			return false, err
		}
	}
	status := "active"
	action := "approved"
	if in.Decision == "decline" {
		status = "declined"
		action = "declined"
	}
	res, err := tx.ExecContext(ctx, `UPDATE group_memberships SET status=$3,decided_at=$4,decided_by=$5 WHERE group_id=$1 AND account_id=$2 AND status='pending'`, groupID, target, status, s.now(), actor)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		var current string
		err = tx.QueryRowContext(ctx, `SELECT status FROM group_memberships WHERE group_id=$1 AND account_id=$2`, groupID, target).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		if err != nil {
			return false, err
		}
		if current == status {
			return false, tx.Commit()
		}
		return false, ErrInvalid
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO group_audit_events(group_id,actor_id,target_id,action,reason) VALUES($1,$2,$3,$4,$5)`, groupID, actor, target, action, in.Reason); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) PendingRequests(ctx context.Context, actor, groupID string) ([]JoinRequest, error) {
	if !validID(actor) || !validID(groupID) {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var audience string
	err = tx.QueryRowContext(ctx, `SELECT audience FROM groups WHERE id=$1 AND archived_at IS NULL`, groupID).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if audience == "leadership" {
		eligible, e := leadershipEligible(ctx, tx, actor)
		if e != nil {
			return nil, e
		}
		if !eligible {
			return nil, ErrNotFound
		}
	}
	err = access.CheckTx(ctx, tx, actor, "groups.members.manage", "")
	if err != nil {
		if !errors.Is(err, access.ErrForbidden) {
			return nil, err
		}
		err = access.CheckTx(ctx, tx, actor, "ministry.members.manage", groupID)
		if err != nil {
			if errors.Is(err, access.ErrForbidden) {
				return nil, ErrForbidden
			}
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.account_id,a.first_name,a.last_name,m.requested_at FROM group_memberships m JOIN accounts a ON a.id=m.account_id WHERE m.group_id=$1 AND m.status='pending' AND a.email_verified_at IS NOT NULL ORDER BY m.requested_at,m.account_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []JoinRequest{}
	for rows.Next() {
		var v JoinRequest
		if err = rows.Scan(&v.AccountID, &v.FirstName, &v.LastName, &v.RequestedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Service) Leave(ctx context.Context, actor, groupID string) error {
	if !validID(actor) || !validID(groupID) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE group_memberships SET status='left',decided_at=$3,decided_by=$2 WHERE group_id=$1 AND account_id=$2 AND status IN ('active','pending')`, groupID, actor, s.now())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO group_audit_events(group_id,actor_id,target_id,action) VALUES($1,$2,$2,'left')`, groupID, actor); err != nil {
			return err
		}
	}
	return tx.Commit()
}

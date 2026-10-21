package groups

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func groupFixture(t *testing.T) (*sql.DB, *Service, string, string, string) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	create := func(email, role string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,password_hash) VALUES($1,'Test','Member',now(),'test-hash') RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,'')`, id, role); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	admin := create("admin@example.invalid", "super_administrator")
	member := create("member@example.invalid", "")
	other := create("other@example.invalid", "")
	return db, NewService(NewRepository(db), nil), admin, member, other
}

func TestGroupCreationMembershipApprovalAndLeave(t *testing.T) {
	db, service, admin, member, _ := groupFixture(t)
	ctx := context.Background()
	group, err := service.Create(ctx, admin, CreateInput{
		Name: "Choir", Description: "Sunday worship team", Audience: "church", JoinPolicy: "approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.ID == "" || group.Name != "Choir" || group.Audience != "church" || group.JoinPolicy != "approval" {
		t.Fatalf("created group: %+v", group)
	}

	page, err := service.List(ctx, member, Options{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != group.ID || page.Items[0].MembershipStatus != "" {
		t.Fatalf("member group list: %+v %v", page, err)
	}
	request, err := service.Join(ctx, member, group.ID)
	if err != nil || request.Status != "pending" {
		t.Fatalf("join request: %+v %v", request, err)
	}
	request, err = service.Join(ctx, member, group.ID)
	if err != nil || request.Status != "pending" {
		t.Fatalf("repeat request must be idempotent: %+v %v", request, err)
	}

	if _, err = service.Decide(ctx, member, group.ID, member, DecisionInput{Decision: "approve", Reason: "Self approval"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member approved own request: %v", err)
	}
	leader := "44444444-4444-4444-8444-444444444444"
	if _, err = db.Exec(`INSERT INTO accounts(id,email,first_name,last_name,email_verified_at) VALUES($1,'leader@example.invalid','Group','Leader',now())`, leader); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, group.ID); err != nil {
		t.Fatal(err)
	}
	changed, err := service.Decide(ctx, leader, group.ID, member, DecisionInput{Decision: "approve", Reason: "Reviewed request"})
	if err != nil || !changed {
		t.Fatalf("leader approval: changed=%v err=%v", changed, err)
	}
	changed, err = service.Decide(ctx, leader, group.ID, member, DecisionInput{Decision: "approve", Reason: "Repeated decision"})
	if err != nil || changed {
		t.Fatalf("repeat decision must be idempotent: changed=%v err=%v", changed, err)
	}

	page, err = service.List(ctx, member, Options{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].MembershipStatus != "active" || page.Items[0].MemberCount != 1 {
		t.Fatalf("approved membership: %+v %v", page, err)
	}
	if err = service.Leave(ctx, member, group.ID); err != nil {
		t.Fatal(err)
	}
	page, err = service.List(ctx, member, Options{Limit: 20})
	if err != nil || page.Items[0].MembershipStatus != "left" || page.Items[0].MemberCount != 0 {
		t.Fatalf("leave group: %+v %v", page, err)
	}
	var audits int
	if err = db.QueryRow(`SELECT count(*) FROM group_audit_events WHERE group_id=$1`, group.ID).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("expected request and decision audit only; count=%d err=%v", audits, err)
	}
}

func TestGroupAudiencesAndAuthorization(t *testing.T) {
	db, service, admin, member, other := groupFixture(t)
	ctx := context.Background()
	church, err := service.Create(ctx, admin, CreateInput{Name: "Choir", Audience: "church", JoinPolicy: "open"})
	if err != nil {
		t.Fatal(err)
	}
	leadership, err := service.Create(ctx, admin, CreateInput{Name: "Church Leadership", Audience: "leadership", JoinPolicy: "approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Create(ctx, admin, CreateInput{Name: "Second Leadership Group", Audience: "leadership", JoinPolicy: "approval"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("created a second leadership-only group: %v", err)
	}
	page, err := service.List(ctx, member, Options{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != church.ID {
		t.Fatalf("wrong groups visible to member: %+v %v", page, err)
	}
	ownerPage, err := service.List(ctx, admin, Options{Limit: 20})
	if err != nil || len(ownerPage.Items) != 2 {
		t.Fatalf("group creator cannot see created groups: %+v %v", ownerPage, err)
	}
	if _, err = service.Join(ctx, member, leadership.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("church member accessed leadership group: %v", err)
	}
	ministryHead := "66666666-6666-4666-8666-666666666666"
	if _, err = db.Exec(`INSERT INTO accounts(id,email,first_name,last_name,email_verified_at) VALUES($1,'head@example.invalid','Ministry','Head',now())`, ministryHead); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, ministryHead, church.ID); err != nil {
		t.Fatal(err)
	}
	leadershipPage, err := service.List(ctx, ministryHead, Options{Limit: 20})
	if err != nil || len(leadershipPage.Items) != 2 {
		t.Fatalf("ministry head should see leadership and church groups: %+v %v", leadershipPage, err)
	}
	if request, e := service.Join(ctx, ministryHead, leadership.ID); e != nil || request.Status != "pending" {
		t.Fatalf("ministry head should request leadership membership: %+v %v", request, e)
	}
	if _, err = service.Decide(ctx, admin, leadership.ID, ministryHead, DecisionInput{Decision: "approve", Reason: "Leadership appointment"}); err != nil {
		t.Fatalf("approve leadership member: %v", err)
	}
	leadershipRoster, err := service.Members(ctx, ministryHead, leadership.ID)
	if err != nil || len(leadershipRoster) != 1 || leadershipRoster[0].AccountID != ministryHead {
		t.Fatalf("eligible ministry leader should read leadership roster: %+v %v", leadershipRoster, err)
	}
	staff := "77777777-7777-4777-8777-777777777777"
	if _, err = db.Exec(`INSERT INTO accounts(id,email,first_name,last_name,email_verified_at) VALUES($1,'staff@example.invalid','Church','Staff',now())`, staff); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'staff','')`, staff); err != nil {
		t.Fatal(err)
	}
	staffPage, err := service.List(ctx, staff, Options{Limit: 20})
	if err != nil || len(staffPage.Items) != 1 || staffPage.Items[0].ID != church.ID {
		t.Fatalf("staff should not see leadership-only group: %+v %v", staffPage, err)
	}
	if _, err = service.PendingRequests(ctx, staff, leadership.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("staff read leadership-group requests by guessed ID: %v", err)
	}
	if _, err = service.Members(ctx, staff, leadership.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("staff read leadership-group roster by guessed ID: %v", err)
	}
	if _, err = service.Decide(ctx, staff, leadership.ID, ministryHead, DecisionInput{Decision: "approve", Reason: "No leadership access"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("staff decided leadership-group request by guessed ID: %v", err)
	}
	if _, err = service.Create(ctx, member, CreateInput{Name: "Forged", Audience: "church", JoinPolicy: "open"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member created group: %v", err)
	}
	if _, err = service.Decide(ctx, other, church.ID, member, DecisionInput{Decision: "approve", Reason: "No authority"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unassigned account managed membership: %v", err)
	}
	if err = service.Leave(ctx, other, church.ID); err != nil {
		t.Fatalf("leaving a group without membership should be idempotent: %v", err)
	}
}

func TestMinistryLeaderCannotCrossGroupScope(t *testing.T) {
	db, service, admin, member, leader := groupFixture(t)
	ctx := context.Background()
	one, err := service.Create(ctx, admin, CreateInput{Name: "Group One", Audience: "church", JoinPolicy: "approval"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := service.Create(ctx, admin, CreateInput{Name: "Group Two", Audience: "church", JoinPolicy: "approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Join(ctx, member, two.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, one.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.PendingRequests(ctx, leader, two.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("leader read other group's requests: %v", err)
	}
	if _, err = service.Decide(ctx, leader, two.ID, member, DecisionInput{Decision: "approve", Reason: "No scope"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("leader decided in another group: %v", err)
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM group_memberships WHERE group_id=$1 AND account_id=$2`, two.ID, member).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("cross-group decision changed membership: %q %v", status, err)
	}
}

func TestConcurrentMembershipDecisionsAreAtomic(t *testing.T) {
	db, service, admin, member, _ := groupFixture(t)
	ctx := context.Background()
	g, err := service.Create(ctx, admin, CreateInput{Name: "Requests", Audience: "church", JoinPolicy: "approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Join(ctx, member, g.ID); err != nil {
		t.Fatal(err)
	}
	leader := "55555555-5555-4555-8555-555555555555"
	if _, err = db.Exec(`INSERT INTO accounts(id,email,first_name,last_name,email_verified_at) VALUES($1,'leader2@example.invalid','Leader','Two',now())`, leader); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, g.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, decision := range []string{"approve", "decline"} {
		wg.Add(1)
		go func(decision string) {
			defer wg.Done()
			_, e := service.Decide(ctx, leader, g.ID, member, DecisionInput{Decision: decision, Reason: "Concurrent review"})
			errs <- e
		}(decision)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected decision error: %v", err)
		}
	}
	var decisions, pending int
	if err = db.QueryRow(`SELECT count(*) FROM group_audit_events WHERE group_id=$1 AND action IN ('approved','declined')`, g.ID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM group_memberships WHERE group_id=$1 AND account_id=$2 AND status='pending'`, g.ID, member).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || pending != 0 {
		t.Fatalf("concurrent decisions were not atomic: audits=%d pending=%d", decisions, pending)
	}
}

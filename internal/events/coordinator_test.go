package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceAssignsCoordinatorsOnlyFromAuthorizedActiveChurchGroup(t *testing.T) {
	db, repo, creator, firstMember, secondMember, now := repositoryFixture(t)
	ctx := context.Background()
	groupID := "44444444-4444-4444-8444-444444444444"
	otherGroupID := "55555555-5555-4555-8555-555555555555"
	if _, err := db.Exec(`INSERT INTO groups(id,name,audience,join_policy,created_by) VALUES($1,'Choir','church','approval',$3),($2,'Youth','church','approval',$3)`, groupID, otherGroupID, creator); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry'),($2,'ministry')`, groupID, otherGroupID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active'),($1,$3,'active'),($4,$5,'active')`, groupID, firstMember, secondMember, otherGroupID, creator); err != nil {
		t.Fatal(err)
	}
	var leader string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('coordinator-leader@example.invalid','Group','Leader',now()) RETURNING id`).Scan(&leader); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, leader, groupID); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, func() time.Time { return now })
	draft, err := service.Create(ctx, creator, eventInput(now, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AssignCoordinator(ctx, leader, draft.ID, groupID, firstMember); err != nil {
		t.Fatalf("leader assigning active own-group member: %v", err)
	}
	if _, err = service.AssignCoordinator(ctx, leader, draft.ID, otherGroupID, creator); !errors.Is(err, ErrForbidden) {
		t.Fatalf("leader assigned outside own scope: %v", err)
	}
	if _, err = service.AssignCoordinator(ctx, leader, draft.ID, groupID, creator); !errors.Is(err, ErrForbidden) {
		t.Fatalf("leader assigned a non-roster member: %v", err)
	}
	if _, err = service.Update(ctx, creator, draft.ID, UpdateInput{Title: draft.Title, Description: draft.Description, Location: draft.Location, StartsAt: draft.StartsAt, TimeZone: draft.TimeZone, Status: "published", Version: draft.Version}); err != nil {
		t.Fatal(err)
	}
	items, err := service.Coordinators(ctx, draft.ID)
	if err != nil || len(items) != 1 || items[0].AccountID != firstMember || items[0].GroupID != groupID {
		t.Fatalf("coordinators=%+v err=%v", items, err)
	}
	if err = service.RemoveCoordinator(ctx, leader, draft.ID, firstMember); err != nil {
		t.Fatalf("leader removing own-scope coordinator: %v", err)
	}
	items, err = service.Coordinators(ctx, draft.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("coordinator removal left assignments %+v err=%v", items, err)
	}
	_ = secondMember
}

func TestGlobalEventsManagerMayAssignActiveRosterMemberAndNotUnverifiedMember(t *testing.T) {
	db, repo, creator, activeMember, _, now := repositoryFixture(t)
	ctx := context.Background()
	groupID := "44444444-4444-4444-8444-444444444444"
	if _, err := db.Exec(`INSERT INTO groups(id,name,audience,join_policy,created_by) VALUES($1,'Choir','church','approval',$2)`, groupID, creator); err != nil {
		t.Fatal(err)
	}
	var unverified string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name) VALUES('unverified@example.invalid','Un','Verified') RETURNING id`).Scan(&unverified); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active'),($1,$3,'active')`, groupID, activeMember, unverified); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, func() time.Time { return now })
	draft, err := service.Create(ctx, creator, eventInput(now, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AssignCoordinator(ctx, creator, draft.ID, groupID, activeMember); err != nil {
		t.Fatalf("global manager assigning active verified roster member: %v", err)
	}
	if _, err = service.AssignCoordinator(ctx, creator, draft.ID, groupID, unverified); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unverified coordinator assignment error=%v", err)
	}
}

func TestServiceRejectsLeadershipAndArchivedGroupCoordinatorAssignment(t *testing.T) {
	db, repo, creator, member, _, now := repositoryFixture(t)
	ctx := context.Background()
	churchID := "44444444-4444-4444-8444-444444444444"
	leadershipID := "55555555-5555-4555-8555-555555555555"
	if _, err := db.Exec(`INSERT INTO groups(id,name,audience,join_policy,created_by,archived_at) VALUES($1,'Choir','church','approval',$3,now()),($2,'Leadership','leadership','closed',$3,NULL)`, churchID, leadershipID, creator); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$3,'active'),($2,$3,'active')`, churchID, leadershipID, member); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, func() time.Time { return now })
	draft, err := service.Create(ctx, creator, eventInput(now, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, groupID := range []string{churchID, leadershipID} {
		if _, err = service.AssignCoordinator(ctx, creator, draft.ID, groupID, member); !errors.Is(err, ErrForbidden) {
			t.Fatalf("group %s eligibility error=%v", groupID, err)
		}
	}
}

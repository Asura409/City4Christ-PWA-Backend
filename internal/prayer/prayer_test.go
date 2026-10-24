package prayer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func prayerFixture(t *testing.T) (context.Context, *Service, string, string, string, string) {
	t.Helper()
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	account := func(email string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Prayer','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner := account("owner@example.invalid")
	member := account("member@example.invalid")
	stranger := account("stranger@example.invalid")
	var group string
	if err := db.QueryRow(`INSERT INTO groups(name,audience,join_policy,created_by) VALUES('Choir','church','open',$1) RETURNING id`, owner).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active'),($1,$3,'active')`, group, member, owner); err != nil {
		t.Fatal(err)
	}
	return ctx, NewService(NewRepository(db)), owner, member, stranger, group
}

func TestRepositoryEnforcesVisibilityAndAnonymousDisplay(t *testing.T) {
	ctx, service, owner, member, stranger, group := prayerFixture(t)
	community, err := service.Create(ctx, owner, CreateInput{Body: "Community request", Visibility: "community", Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	private, err := service.Create(ctx, owner, CreateInput{Body: "Private request", Visibility: "private", Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	groupRequest, err := service.Create(ctx, owner, CreateInput{Body: "Group request", Visibility: "group", GroupID: group})
	if err != nil {
		t.Fatal(err)
	}
	if private.Author != "You" || !private.IsOwner {
		t.Fatalf("owner anonymous affordance: %+v", private)
	}
	outsiderPage, err := service.List(ctx, stranger, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(outsiderPage.Items) != 1 || outsiderPage.Items[0].ID != community.ID || outsiderPage.Items[0].Author != "Anonymous" {
		t.Fatalf("outsider visibility: %+v", outsiderPage)
	}
	memberPage, err := service.List(ctx, member, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(memberPage.Items) != 2 || !containsRequest(memberPage.Items, groupRequest.ID) || containsRequest(memberPage.Items, private.ID) {
		t.Fatalf("active group member visibility: %+v", memberPage.Items)
	}
	if _, err := service.Get(ctx, stranger, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private request disclosed to stranger: %v", err)
	}
	if _, err := service.Get(ctx, stranger, groupRequest.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group request disclosed to nonmember: %v", err)
	}
	visibleAnon, err := service.Get(ctx, member, community.ID)
	if err != nil || visibleAnon.Author != "Anonymous" || visibleAnon.IsOwner {
		t.Fatalf("anonymous community attribution leaked: %+v %v", visibleAnon, err)
	}
	ownerView, err := service.Get(ctx, owner, community.ID)
	if err != nil || ownerView.Author != "You" || !ownerView.IsOwner {
		t.Fatalf("owner affordance missing for anonymous request: %+v %v", ownerView, err)
	}
}

func TestPrayerAcknowledgementIsIdempotentAndScoped(t *testing.T) {
	ctx, service, owner, member, stranger, group := prayerFixture(t)
	request, err := service.Create(ctx, owner, CreateInput{Body: "Please pray", Visibility: "group", GroupID: group, Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.Pray(ctx, member, request.ID); err != nil {
			t.Fatalf("acknowledgement %d: %v", i, err)
		}
	}
	got, err := service.Get(ctx, member, request.ID)
	if err != nil || !got.HasPrayed || got.PrayerCount != 1 || got.Author != "Anonymous" || got.IsOwner {
		t.Fatalf("member view after duplicate acknowledgement: %+v %v", got, err)
	}
	if _, err := service.Pray(ctx, stranger, request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nonmember acknowledged hidden request: %v", err)
	}
	ownerView, err := service.Get(ctx, owner, request.ID)
	if err != nil || ownerView.Author != "You" || !ownerView.IsOwner || ownerView.PrayerCount != 1 {
		t.Fatalf("anonymous owner view leaked or lost affordance: %+v %v", ownerView, err)
	}
}

func TestPrayerOwnerLifecycleAndPagination(t *testing.T) {
	ctx, service, owner, member, stranger, _ := prayerFixture(t)
	created := make([]Request, 0, 3)
	for i := 0; i < 3; i++ {
		item, err := service.Create(ctx, owner, CreateInput{Body: fmt.Sprintf("Request %d", i), Visibility: "community"})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, item)
	}
	first, err := service.List(ctx, member, Options{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := service.List(ctx, member, Options{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	if first.Items[1].ID == second.Items[0].ID {
		t.Fatal("request repeated across page boundary")
	}
	if changed, err := service.SetStatus(ctx, stranger, created[0].ID, "answered"); !errors.Is(err, ErrNotFound) || changed {
		t.Fatalf("non-owner changed request: changed=%v err=%v", changed, err)
	}
	if changed, err := service.SetStatus(ctx, owner, created[0].ID, "answered"); err != nil || !changed {
		t.Fatalf("owner answer: changed=%v err=%v", changed, err)
	}
	if changed, err := service.SetStatus(ctx, owner, created[0].ID, "answered"); err != nil || changed {
		t.Fatalf("repeated answer should be idempotent: changed=%v err=%v", changed, err)
	}
	answered, err := service.List(ctx, member, Options{Status: "answered"})
	if err != nil || len(answered.Items) != 1 || answered.Items[0].ID != created[0].ID {
		t.Fatalf("answered filter: %+v %v", answered, err)
	}
	if changed, err := service.SetStatus(ctx, owner, created[0].ID, "archived"); err != nil || !changed {
		t.Fatalf("archive: changed=%v err=%v", changed, err)
	}
	if _, err := service.Get(ctx, owner, created[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived request remained visible in detail: %v", err)
	}
	if _, err := service.Create(ctx, owner, CreateInput{Body: strings.Repeat("x", 5001), Visibility: "community"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized prayer accepted: %v", err)
	}
}

func TestGroupPrayerRequiresActiveMembership(t *testing.T) {
	ctx, service, owner, member, stranger, group := prayerFixture(t)
	if _, err := service.Create(ctx, stranger, CreateInput{Body: "Not a member", Visibility: "group", GroupID: group}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nonmember created group prayer: %v", err)
	}
	if _, err := service.Create(ctx, owner, CreateInput{Body: "Missing group", Visibility: "group"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("group request without group id accepted: %v", err)
	}
	if _, err := service.Create(ctx, owner, CreateInput{Body: "Group id on community", Visibility: "community", GroupID: group}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("community request accepted group id: %v", err)
	}
	request, err := service.Create(ctx, owner, CreateInput{Body: "Membership can change", Visibility: "group", GroupID: group})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, member, request.ID); err != nil {
		t.Fatalf("active member could not read group request: %v", err)
	}
	if _, err := service.repo.db.ExecContext(ctx, `UPDATE group_memberships SET status='left' WHERE group_id=$1 AND account_id=$2`, group, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, owner, request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("former group member still reads request: %v", err)
	}
	if _, err := service.SetStatus(ctx, owner, request.ID, "answered"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("former group member still manages request: %v", err)
	}
	if _, err := service.Pray(ctx, owner, request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("former group member still acknowledges request: %v", err)
	}
	page, err := service.List(ctx, owner, Options{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if containsRequest(page.Items, request.ID) {
		t.Fatalf("former group member still sees request: %+v", page.Items)
	}
}

func containsRequest(items []Request, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

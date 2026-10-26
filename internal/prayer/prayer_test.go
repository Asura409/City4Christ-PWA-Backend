package prayer

import (
	"context"
	"database/sql"
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
	if _, err := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, group); err != nil {
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

func TestEncouragementsUsePrayerVisibilityAndAreListedInOrder(t *testing.T) {
	ctx, service, owner, member, stranger, group := prayerFixture(t)
	community, err := service.Create(ctx, owner, CreateInput{Body: "Community", Visibility: "community", Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	private, err := service.Create(ctx, owner, CreateInput{Body: "Private", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	groupRequest, err := service.Create(ctx, owner, CreateInput{Body: "Group", Visibility: "group", GroupID: group})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateEncouragement(ctx, member, community.ID, EncouragementInput{Body: "Praying for you"})
	if err != nil || first.Author == "" || first.IsOwner {
		t.Fatalf("create community encouragement: %+v %v", first, err)
	}
	second, err := service.CreateEncouragement(ctx, owner, private.ID, EncouragementInput{Body: "A private reply"})
	if err != nil || second.Author != "You" || !second.IsOwner {
		t.Fatalf("owner private reply: %+v %v", second, err)
	}
	var notices int
	if err := service.repo.db.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE account_id=$1 AND kind='prayer.encouragement'`, owner).Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("owner notice missing or self-encouragement notified: count=%d err=%v", notices, err)
	}
	privateReplies, err := service.ListEncouragements(ctx, owner, private.ID)
	if err != nil || len(privateReplies) != 1 || privateReplies[0].Author != "You" || !privateReplies[0].IsOwner {
		t.Fatalf("private owner could not read own encouragement: %+v %v", privateReplies, err)
	}
	if _, err := service.CreateEncouragement(ctx, stranger, private.ID, EncouragementInput{Body: "Should not leak"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private request disclosed by encouragement write: %v", err)
	}
	if _, err := service.CreateEncouragement(ctx, stranger, groupRequest.ID, EncouragementInput{Body: "Should not leak"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group request disclosed by encouragement write: %v", err)
	}
	listed, err := service.ListEncouragements(ctx, member, community.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != first.ID || listed[0].Author == "" {
		t.Fatalf("visible encouragement list: %+v %v", listed, err)
	}
	if _, err := service.ListEncouragements(ctx, stranger, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private request disclosed by encouragement list: %v", err)
	}
	if _, err := service.ListEncouragements(ctx, stranger, groupRequest.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group request disclosed by encouragement list: %v", err)
	}
	if _, err := service.CreateEncouragement(ctx, member, community.ID, EncouragementInput{Body: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty encouragement accepted: %v", err)
	}
	if changed, err := service.SetStatus(ctx, owner, community.ID, "archived"); err != nil || !changed {
		t.Fatalf("archive: %v %v", changed, err)
	}
	if _, err := service.CreateEncouragement(ctx, member, community.ID, EncouragementInput{Body: "Too late"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived prayer accepted encouragement: %v", err)
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

func TestReportsAreScopedAndModerationSoftArchivesWithAudit(t *testing.T) {
	ctx, service, owner, member, stranger, group := prayerFixture(t)
	community, err := service.Create(ctx, owner, CreateInput{Body: "Community report target", Visibility: "community", Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	groupRequest, err := service.Create(ctx, owner, CreateInput{Body: "Group report target", Visibility: "group", GroupID: group})
	if err != nil {
		t.Fatal(err)
	}
	private, err := service.Create(ctx, owner, CreateInput{Body: "Private and confidential", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	groupLeader := createPrayerAccount(t, service.repo.db, "leader@example.invalid")
	if _, err = service.repo.db.ExecContext(ctx, `INSERT INTO account_roles(account_id,role,scope) VALUES($1,'ministry_leader',$2)`, groupLeader, group); err != nil {
		t.Fatal(err)
	}
	if _, err = service.repo.db.ExecContext(ctx, `INSERT INTO group_memberships(group_id,account_id,status) VALUES($1,$2,'active')`, group, groupLeader); err != nil {
		t.Fatal(err)
	}
	globalStaff := createPrayerAccount(t, service.repo.db, "moderator@example.invalid")
	if _, err = service.repo.db.ExecContext(ctx, `INSERT INTO account_roles(account_id,role,scope) VALUES($1,'staff','')`, globalStaff); err != nil {
		t.Fatal(err)
	}

	communityReport, err := service.Report(ctx, member, community.ID, ReportInput{Reason: "safety", Details: "Please review this post."})
	if err != nil {
		t.Fatal(err)
	}
	groupReport, err := service.Report(ctx, member, groupRequest.ID, ReportInput{Reason: "inappropriate", Details: "Needs group review."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Report(ctx, stranger, private.ID, ReportInput{Reason: "safety"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private request could be reported: %v", err)
	}
	if _, err = service.ListReports(ctx, stranger, Options{Status: "pending"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ordinary member read moderation queue: %v", err)
	}
	staffQueue, err := service.ListReports(ctx, globalStaff, Options{Status: "pending"})
	if err != nil || len(staffQueue.Items) != 1 || staffQueue.Items[0].ID != communityReport.ID || staffQueue.Items[0].RequestBody != community.Body {
		t.Fatalf("global community queue exposed wrong scope: %+v %v", staffQueue.Items, err)
	}
	leaderQueue, err := service.ListReports(ctx, groupLeader, Options{Status: "pending"})
	if err != nil || len(leaderQueue.Items) != 1 || leaderQueue.Items[0].ID != groupReport.ID {
		t.Fatalf("group leader queue exposed wrong scope: %+v %v", leaderQueue.Items, err)
	}
	if _, err = service.ReviewReport(ctx, globalStaff, groupReport.ID, ReviewInput{Decision: "hide", Reason: "Wrong group scope."}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("global community moderator accessed group report: %v", err)
	}
	if _, err = service.ReviewReport(ctx, groupLeader, communityReport.ID, ReviewInput{Decision: "dismiss", Reason: "Wrong group scope."}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group moderator accessed community report: %v", err)
	}
	if _, err = service.ReviewReport(ctx, groupLeader, groupReport.ID, ReviewInput{Decision: "hide", Reason: "Violates group guidelines."}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(ctx, member, groupRequest.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("soft-archived request remained public: %v", err)
	}
	var reportState, archiveReason string
	if err = service.repo.db.QueryRowContext(ctx, `SELECT r.status,p.archive_reason FROM prayer_reports r JOIN prayer_requests p ON p.id=r.request_id WHERE r.id=$1`, groupReport.ID).Scan(&reportState, &archiveReason); err != nil {
		t.Fatal(err)
	}
	if reportState != "resolved" || archiveReason != "moderation" {
		t.Fatalf("moderation state=%q archive reason=%q", reportState, archiveReason)
	}
	var auditCount int
	if err = service.repo.db.QueryRowContext(ctx, `SELECT count(*) FROM prayer_moderation_events WHERE report_id=$1 AND actor_id=$2 AND action='hidden'`, groupReport.ID, groupLeader).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("moderation audit count=%d err=%v", auditCount, err)
	}
	if _, err = service.ReviewReport(ctx, groupLeader, groupReport.ID, ReviewInput{Decision: "dismiss", Reason: "Already resolved."}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolved report remained actionable: %v", err)
	}
}

func TestPrayerExpiryArchivesContentWithoutDeletingAndAudits(t *testing.T) {
	ctx, service, owner, member, _, _ := prayerFixture(t)
	request, err := service.Create(ctx, owner, CreateInput{Body: "An old but retained request", Visibility: "community"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.repo.db.ExecContext(ctx, `UPDATE prayer_requests SET expires_at=now()-interval '1 second' WHERE id=$1`, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.List(ctx, member, Options{Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(ctx, owner, request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired request remained visible: %v", err)
	}
	var status, archiveReason, body, action string
	if err = service.repo.db.QueryRowContext(ctx, `SELECT p.status,p.archive_reason,p.body,e.action FROM prayer_requests p JOIN prayer_moderation_events e ON e.request_id=p.id WHERE p.id=$1`, request.ID).Scan(&status, &archiveReason, &body, &action); err != nil {
		t.Fatal(err)
	}
	if status != "archived" || archiveReason != "expiry" || body != request.Body || action != "expired" {
		t.Fatalf("expiry status=%q reason=%q body=%q audit=%q", status, archiveReason, body, action)
	}
}

func createPrayerAccount(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), `INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Prayer','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

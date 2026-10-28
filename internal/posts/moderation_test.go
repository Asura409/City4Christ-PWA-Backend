package posts

import (
	"context"
	"errors"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestCommunityReportsRequireScopeAndModerationIsAudited(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	const leaderScope = "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec(`INSERT INTO access_scopes(id,kind) VALUES($1,'ministry')`, leaderScope); err != nil {
		t.Fatal(err)
	}
	account := func(email, role, scope string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES($1,'Community','Member',now()) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if role != "" {
			if _, err := db.Exec(`INSERT INTO account_roles(account_id,role,scope) VALUES($1,$2,$3)`, id, role, scope); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	author := account("moderation-author@example.invalid", "", "")
	reporter := account("moderation-reporter@example.invalid", "", "")
	member := account("moderation-member@example.invalid", "", "")
	staff := account("moderation-staff@example.invalid", "staff", "")
	leader := account("moderation-leader@example.invalid", "ministry_leader", leaderScope)
	posts := NewService(NewRepository(db))
	moderation := NewModerationService(NewModerationRepository(db), access.NewService(db))
	post, err := posts.Create(ctx, author, CreateInput{Body: "A harmful post"})
	if err != nil {
		t.Fatal(err)
	}
	commentService := NewCommentService(NewCommentRepository(db))
	comment, err := commentService.Create(ctx, author, post.ID, CommentInput{Body: "A harmful comment"})
	if err != nil {
		t.Fatal(err)
	}
	postReport, err := moderation.Report(ctx, reporter, ReportInput{TargetType: "post", TargetID: post.ID, Reason: "harassment", Details: "Targets a member."})
	if err != nil || postReport.Status != "pending" {
		t.Fatalf("report post: %+v %v", postReport, err)
	}
	commentReport, err := moderation.Report(ctx, reporter, ReportInput{TargetType: "comment", TargetID: comment.ID, Reason: "inappropriate"})
	if err != nil || commentReport.Status != "pending" {
		t.Fatalf("report comment: %+v %v", commentReport, err)
	}
	if _, err := moderation.Report(ctx, reporter, ReportInput{TargetType: "post", TargetID: post.ID, Reason: "spam"}); !errors.Is(err, ErrReportConflict) {
		t.Fatalf("duplicate report accepted: %v", err)
	}
	if _, err := moderation.Report(ctx, author, ReportInput{TargetType: "post", TargetID: post.ID, Reason: "spam"}); !errors.Is(err, ErrReportConflict) {
		t.Fatalf("author reported own post: %v", err)
	}
	if _, err := moderation.Report(ctx, reporter, ReportInput{TargetType: "post", TargetID: post.ID, Reason: "made-up"}); !errors.Is(err, ErrReportInvalid) {
		t.Fatalf("unknown report reason accepted: %v", err)
	}
	if _, err := moderation.ListReports(ctx, member, ReportOptions{Status: "pending"}); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("ordinary member read moderation queue: %v", err)
	}
	if _, err := moderation.ListReports(ctx, leader, ReportOptions{Status: "pending"}); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("scoped leader moderated church-wide feed: %v", err)
	}
	queue, err := moderation.ListReports(ctx, staff, ReportOptions{Status: "pending", Limit: 1})
	if err != nil || len(queue.Items) != 1 || queue.NextCursor == "" {
		t.Fatalf("staff queue: %+v %v", queue, err)
	}
	next, err := moderation.ListReports(ctx, staff, ReportOptions{Status: "pending", Limit: 1, Cursor: queue.NextCursor})
	if err != nil || len(next.Items) != 1 || queue.Items[0].ID == next.Items[0].ID {
		t.Fatalf("second report page: %+v %v", next, err)
	}
	if err := moderation.Review(ctx, member, postReport.ID, ReviewInput{Decision: "hide", Reason: "Unauthorized"}); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("ordinary member reviewed report: %v", err)
	}
	if err := moderation.Review(ctx, staff, postReport.ID, ReviewInput{Decision: "hide", Reason: "Harassment confirmed."}); err != nil {
		t.Fatalf("hide reported post: %v", err)
	}
	feed, err := posts.List(ctx, Options{}, reporter)
	if err != nil || len(feed.Items) != 0 {
		t.Fatalf("hidden post still in feed: %+v %v", feed, err)
	}
	if _, err := commentService.List(ctx, reporter, post.ID, CommentOptions{}); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("hidden post comments still visible: %v", err)
	}
	var postDeleted bool
	var postBody, reportStatus, action string
	if err := db.QueryRow(`SELECT p.deleted_at IS NOT NULL,p.body,r.status,e.action FROM church_posts p JOIN post_content_reports r ON r.post_id=p.id JOIN post_moderation_events e ON e.report_id=r.id WHERE r.id=$1`, postReport.ID).Scan(&postDeleted, &postBody, &reportStatus, &action); err != nil {
		t.Fatal(err)
	}
	if !postDeleted || postBody != "[Post removed by moderation]" || reportStatus != "resolved" || action != "hidden" {
		t.Fatalf("moderation state: postDeleted=%v body=%q report=%s action=%s", postDeleted, postBody, reportStatus, action)
	}
	var relatedAudits int
	if err := db.QueryRow(`SELECT count(*) FROM post_moderation_events WHERE actor_id=$1 AND action='hidden' AND target_id IN ($2,$3)`, staff, post.ID, comment.ID).Scan(&relatedAudits); err != nil || relatedAudits != 2 {
		t.Fatalf("related hide events=%d err=%v", relatedAudits, err)
	}

	visible, err := posts.Create(ctx, author, CreateInput{Body: "Another post"})
	if err != nil {
		t.Fatal(err)
	}
	dismissedComment, err := commentService.Create(ctx, author, visible.ID, CommentInput{Body: "Dismissed report remains visible"})
	if err != nil {
		t.Fatal(err)
	}
	dismissedReport, err := moderation.Report(ctx, reporter, ReportInput{TargetType: "comment", TargetID: dismissedComment.ID, Reason: "inappropriate"})
	if err != nil {
		t.Fatal(err)
	}
	if err := moderation.Review(ctx, staff, dismissedReport.ID, ReviewInput{Decision: "dismiss", Reason: "Does not violate guidelines."}); err != nil {
		t.Fatalf("dismiss report: %v", err)
	}
	comment, err = commentService.Create(ctx, author, visible.ID, CommentInput{Body: "Reported comment"})
	if err != nil {
		t.Fatal(err)
	}
	commentReport, err = moderation.Report(ctx, reporter, ReportInput{TargetType: "comment", TargetID: comment.ID, Reason: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := moderation.Review(ctx, staff, commentReport.ID, ReviewInput{Decision: "hide", Reason: "Confirmed violation."}); err != nil {
		t.Fatalf("hide reported comment: %v", err)
	}
	page, err := commentService.List(ctx, reporter, visible.ID, CommentOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != dismissedComment.ID {
		t.Fatalf("hidden comment remains visible: %+v %v", page, err)
	}
}

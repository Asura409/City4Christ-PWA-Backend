package profiles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDirectoryPrivacySearchAndPagination(t *testing.T) {
	db, r, actor, other := fixture(t)
	ctx := context.Background()
	s := NewService(r)
	if _, err := db.Exec(`UPDATE accounts SET directory_visible=true,first_name='Grace',last_name='Member' WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	var pending string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,directory_visible) VALUES('pending@example.invalid','Grace','Pending',true) RETURNING id`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	page, err := s.Directory(ctx, actor, DirectoryOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != other {
		t.Fatalf("visibility: %+v %v", page, err)
	}
	raw, _ := json.Marshal(page.Items[0])
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if len(fields) != 3 || fields["email"] != nil || fields["firstName"] != "Grace" {
		t.Fatalf("private fields exposed: %s", raw)
	}
	for _, query := range []string{" grACE ", "grace member", "Member"} {
		page, err = s.Directory(ctx, actor, DirectoryOptions{Query: query})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("search %q: %+v %v", query, page, err)
		}
	}
	for _, query := range []string{"second@example.invalid", "%", "_"} {
		page, err = s.Directory(ctx, actor, DirectoryOptions{Query: query})
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("non-name/wildcard search %q: %+v %v", query, page, err)
		}
	}
	if _, err = s.Directory(ctx, pending, DirectoryOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatal("unverified caller read directory")
	}
	for i := 0; i < 4; i++ {
		if _, err = db.Exec(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,directory_visible) VALUES(gen_random_uuid()::text||'@example.invalid','Grace','Member',now(),true)`); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err = s.Directory(ctx, actor, DirectoryOptions{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range page.Items {
			if seen[member.ID] {
				t.Fatal("duplicate member across pages")
			}
			seen[member.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("missing visible members: %d", len(seen))
	}
	if _, err = db.Exec(`UPDATE accounts SET directory_visible=false WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	page, err = s.Directory(ctx, actor, DirectoryOptions{})
	if err != nil || len(page.Items) != 4 {
		t.Fatal("opt-out ignored")
	}
	for _, member := range page.Items {
		if member.ID == other {
			t.Fatal("opted-out member returned")
		}
	}
	// Replaying a cursor cannot restore a member who has opted out.
	forged, _ := json.Marshal(DirectoryCursor{Version: 1, ID: "00000000-0000-4000-8000-000000000001", Query: ""})
	page, err = s.Directory(ctx, actor, DirectoryOptions{Cursor: base64.RawURLEncoding.EncodeToString(forged)})
	if err != nil || len(page.Items) != 4 {
		t.Fatalf("cursor bypassed visibility: %+v %v", page, err)
	}
	for _, member := range page.Items {
		if member.ID == other || member.ID == pending || member.ID == actor {
			t.Fatal("private/unverified member leaked through cursor")
		}
	}
	members, err := r.ListDirectory(ctx, DirectoryOptions{Limit: 100}, nil)
	if err != nil || len(members) != 4 {
		t.Fatal("repository visibility filter failed")
	}
}
func TestDirectoryRejectsInvalidOptionsAndCursor(t *testing.T) {
	_, r, actor, _ := fixture(t)
	s := NewService(r)
	ctx := context.Background()
	for _, o := range []DirectoryOptions{{Limit: -1}, {Limit: 101}, {Query: strings.Repeat("x", 201)}, {Query: "bad\x00query"}, {Query: string([]byte{255})}, {Cursor: "invalid"}, {Cursor: strings.Repeat("x", 2049)}} {
		if _, err := s.Directory(ctx, actor, o); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatalf("invalid options accepted: %+v %v", o, err)
		}
	}
	encode := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	for _, raw := range []string{`null`, `{}`, `{"version":1,"id":"bad","query":""}`, `{"version":2,"id":"11111111-1111-4111-8111-111111111111","query":""}`, `{"version":1,"id":"11111111-1111-4111-8111-111111111111","query":"","extra":true}`, `{"version":1,"id":"11111111-1111-4111-8111-111111111111","query":"different"}`} {
		if _, err := s.Directory(ctx, actor, DirectoryOptions{Cursor: encode(raw)}); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatalf("invalid cursor accepted: %s %v", raw, err)
		}
	}
}

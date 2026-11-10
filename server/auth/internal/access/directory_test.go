package access

import (
	"context"
	"os"
	"testing"
)

func exerciseDirectory(t *testing.T, r Repository) {
	t.Helper()
	ctx := context.Background()
	d := &Directory{Repo: r}
	owner := User{ID: "owner", Email: "owner@example.com"}
	invitee := User{ID: "invitee", Email: "invitee@example.com"}
	if _, err := d.Bind(ctx, owner); err != nil {
		t.Fatal(err)
	}
	org, err := d.CreateOrg(ctx, owner, "Example")
	if err != nil {
		t.Fatal(err)
	}
	team, err := d.CreateTeam(ctx, owner, org, "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.SetMember(ctx, owner, org, "", owner.Email, "", true); err == nil {
		t.Fatal("last owner removed")
	}
	if err = d.SetMember(ctx, owner, org, "", "pending-owner@example.com", "owner", false); err != nil {
		t.Fatal(err)
	}
	if err = d.SetMember(ctx, owner, org, "", owner.Email, "member", false); err == nil {
		t.Fatal("pending owner allowed last owner demotion")
	}
	if err = d.SetMember(ctx, owner, org, "", "pending-owner@example.com", "", true); err != nil {
		t.Fatal("cannot revoke pending owner", err)
	}
	if err = d.SetMember(ctx, owner, org, "", invitee.Email, "member", false); err != nil {
		t.Fatal(err)
	}
	if err = d.SetMember(ctx, owner, org, team, invitee.Email, "viewer", false); err != nil {
		t.Fatal(err)
	}
	s, err := d.Bind(ctx, invitee)
	if err != nil {
		t.Fatal(err)
	}
	if _, role := TeamRole(s, team, invitee.ID); role != "viewer" {
		t.Fatal("invitation not accepted")
	}
	if err = d.SetMember(ctx, invitee, org, team, invitee.Email, "admin", false); err != ErrForbidden {
		t.Fatal("viewer escalated privileges")
	}
	if err = d.SetMember(ctx, owner, org, team, invitee.Email, "admin", false); err != nil {
		t.Fatal(err)
	}
	if err = d.SetMember(ctx, invitee, org, "", invitee.Email, "owner", false); err != ErrForbidden {
		t.Fatal("team admin escalated organization role")
	}
	if err = d.SetMember(ctx, owner, org, "", invitee.Email, "", true); err != nil {
		t.Fatal(err)
	}
	s, _, err = r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, role := TeamRole(s, team, invitee.ID); role != "" {
		t.Fatal("removed organization member retained team access")
	}
	// A stale writer must not overwrite newer membership data.
	_, revision, err := r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.CreateTeam(ctx, owner, org, "Support"); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.Swap(ctx, revision, s); err != nil || ok {
		t.Fatal("stale state overwrite accepted", err)
	}
}
func TestFileDirectory(t *testing.T) {
	r, err := Open(context.Background(), "file", "", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	exerciseDirectory(t, r)
}

func TestInvitationBindsOnlyCurrentVerifiedEmail(t *testing.T) {
	ctx := context.Background()
	r, err := Open(ctx, "file", "", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(ctx)
	d := &Directory{Repo: r}
	owner := User{ID: "owner", Email: "owner@example.com"}
	old := User{ID: "former-account", Email: "reassigned@example.com"}
	if _, err = d.Bind(ctx, old); err != nil {
		t.Fatal(err)
	}
	org, err := d.CreateOrg(ctx, owner, "Example")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.SetMember(ctx, owner, org, "", old.Email, "member", false); err != nil {
		t.Fatal(err)
	}
	s, _, err := r.Read(ctx)
	if err != nil || s.Orgs[org].Members[old.Email].UserID != "" {
		t.Fatal("cached profile claimed new invitation", err)
	}
	current := User{ID: "current-account", Email: old.Email}
	s, err = d.Bind(ctx, current)
	if err != nil || OrgRole(s, org, current.ID) != "member" || OrgRole(s, org, old.ID) != "" {
		t.Fatal("invitation bound wrong identity", err)
	}
}
func TestDatabaseDirectory(t *testing.T) {
	for _, test := range []struct{ driver, variable string }{{"postgres", "RUNLOOM_TEST_POSTGRES_URL"}, {"mongodb", "RUNLOOM_TEST_MONGODB_URL"}} {
		t.Run(test.driver, func(t *testing.T) {
			url := os.Getenv(test.variable)
			if url == "" {
				t.Skip("disposable database URL not configured")
			}
			r, err := Open(context.Background(), test.driver, url, "runloom_access_test", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close(context.Background())
			exerciseDirectory(t, r)
		})
	}
}

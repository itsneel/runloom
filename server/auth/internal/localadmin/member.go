package localadmin

import (
	"context"
	"fmt"

	"github.com/example/runloom/auth/internal/access"
)

// Deliberately public development credentials; guarded by the local-only CLI.
const MemberEmail = "member@runloom.test"
const MemberPassword = "RunloomMember!123"

func EnsureMemberIdentity(ctx context.Context, base string, reset bool) (access.User, bool, error) {
	return ensureIdentity(ctx, base, reset, MemberEmail, MemberPassword, "Local Member")
}

func EnsureMemberDirectory(ctx context.Context, d *access.Directory, user access.User) error {
	return ensureDirectory(ctx, d, user, MemberEmail, "member", "editor", "local.member.ensured", true)
}

func MemberResult(created, reset bool) string {
	action := "Existing password and membership roles preserved."
	if created {
		action = "Created with the documented local-only default password."
	}
	if reset {
		action = "Restored the documented local-only default password and ended existing sessions; membership roles preserved."
	}
	return fmt.Sprintf("Local team member ready: %s; organization Runloom; team General (default role editor). %s", MemberEmail, action)
}

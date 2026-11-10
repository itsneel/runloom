package access

import (
	"context"
	"errors"

	"github.com/example/runloom/management/internal/consumer"
)

var ErrForbidden = errors.New("permission denied")
var ErrNotFound = errors.New("not found")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{Message: message} }

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}
type Member struct {
	Email  string `json:"email"`
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}
type Team struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Members map[string]Member `json:"members"`
}
type Organization struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Members map[string]Member `json:"members"`
	Teams   map[string]Team   `json:"teams"`
}
type Event struct {
	OrgID     string `json:"org_id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	CreatedAt string `json:"created_at"`
}
type State struct {
	Secrets         map[string]StoredSecret     `json:"secrets,omitempty"`
	ServiceAccounts map[string]consumer.Account `json:"service_accounts"`
	Users           map[string]User             `json:"users"`
	Orgs            map[string]Organization     `json:"organizations"`
	Events          []Event                     `json:"events"`
}
type TeamView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type OrgView struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Role  string     `json:"role"`
	Teams []TeamView `json:"teams"`
}
type Overview struct {
	User          User      `json:"user"`
	Organizations []OrgView `json:"organizations"`
}
type Directory struct {
	Repo Repository
	// BindRemote is set by service composition roots; identity writes stay in Auth.
	BindRemote func(context.Context, User) (State, error)
}

func memberRole(members map[string]Member, user string) string {
	for _, m := range members {
		if m.UserID == user && user != "" {
			return m.Role
		}
	}
	return ""
}

func OrgRole(s State, org, user string) string { return memberRole(s.Orgs[org].Members, user) }

func IsOrgAdmin(role string) bool { return role == "owner" || role == "admin" }

func TeamRole(s State, team, user string) (string, string) {
	for id, o := range s.Orgs {
		t, ok := o.Teams[team]
		if !ok {
			continue
		}
		role := OrgRole(s, id, user)
		if role == "" {
			return id, ""
		}
		if IsOrgAdmin(role) {
			return id, "admin"
		}
		return id, memberRole(t.Members, user)
	}
	return "", ""
}

func (d *Directory) Bind(ctx context.Context, u User) (State, error) {
	if d.BindRemote == nil {
		return State{}, ErrForbidden
	}
	return d.BindRemote(ctx, u)
}

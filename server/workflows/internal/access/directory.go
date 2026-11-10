package access

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/google/uuid"
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
	Connections     map[string]StoredConnection `json:"connections,omitempty"`
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

func NewState() State {
	return State{Users: map[string]User{}, Orgs: map[string]Organization{}, Events: []Event{}, ServiceAccounts: map[string]consumer.Account{}}
}
func (d *Directory) Update(ctx context.Context, fn func(*State) error) error {
	return d.UpdateModule(ctx, "identity", fn)
}

func (d *Directory) UpdateModule(ctx context.Context, scope string, fn func(*State) error) error {
	repo := d.Repo.Module(scope)
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Read only the owning module's update state. Authorization receives a
		// tenancy projection; it is never written into another module's records.
		s, revision, err := repo.ReadOwned(ctx)
		if err != nil {
			return err
		}
		if scope != "identity" {
			identity, _, err := d.Repo.Module("identity").ReadOwned(ctx)
			if err != nil {
				return err
			}
			s.Orgs = identity.Orgs
			s.Users = identity.Users
		}
		if err = fn(&s); err != nil {
			return err
		}
		switch scope {
		case "identity":
			s.Connections = nil
			s.Secrets = nil
			s.ServiceAccounts = nil
		case "secrets":
			s.Users = nil
			s.Orgs = nil
			s.ServiceAccounts = nil
		case "consumers":
			s.Connections = nil
			s.Users = nil
			s.Orgs = nil
			s.Secrets = nil
		}
		ok, err := repo.Swap(ctx, revision, s)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("membership changed concurrently; retry")
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
func IsOrgAdmin(role string) bool              { return role == "owner" || role == "admin" }
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
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return "", invalid("name must contain 1 to 120 characters")
	}
	return name, nil
}
func cleanEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		return "", invalid("valid email required")
	}
	return email, nil
}
func audit(s *State, org, user, action, target string) {
	s.Events = append(s.Events, Event{OrgID: org, Actor: user, Action: action, Target: target, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}
func (d *Directory) Bind(ctx context.Context, u User) (State, error) {
	if d.BindRemote != nil {
		return d.BindRemote(ctx, u)
	}
	email, err := cleanEmail(u.Email)
	if err != nil {
		return State{}, err
	}
	u.Email = email
	s, _, err := d.Repo.Read(ctx)
	if err != nil {
		return s, err
	}
	dirty := s.Users[u.ID] != u
	for _, o := range s.Orgs {
		m, ok := o.Members[u.Email]
		if ok && m.UserID == "" && memberRole(o.Members, u.ID) == "" {
			dirty = true
		}
	}
	if !dirty {
		return s, nil
	}
	err = d.Update(ctx, func(s *State) error {
		s.Users[u.ID] = u
		for id, o := range s.Orgs {
			if m, ok := o.Members[u.Email]; ok && m.UserID == "" {
				// Do not bind a second membership to an already enrolled subject.
				if memberRole(o.Members, u.ID) != "" {
					continue
				}
				m.UserID = u.ID
				o.Members[u.Email] = m
				for tid, t := range o.Teams {
					if tm, ok := t.Members[u.Email]; ok && tm.UserID == "" {
						tm.UserID = u.ID
						t.Members[u.Email] = tm
						o.Teams[tid] = t
					}
				}
				s.Orgs[id] = o
				audit(s, id, u.ID, "membership.accepted", u.Email)
			}
		}
		return nil
	})
	if err != nil {
		return s, err
	}
	s, _, err = d.Repo.Read(ctx)
	return s, err
}
func View(s State, u User) Overview {
	out := Overview{User: u, Organizations: []OrgView{}}
	for id, o := range s.Orgs {
		role := OrgRole(s, id, u.ID)
		if role == "" {
			continue
		}
		v := OrgView{ID: id, Name: o.Name, Role: role, Teams: []TeamView{}}
		for tid, t := range o.Teams {
			_, tr := TeamRole(s, tid, u.ID)
			if tr != "" {
				v.Teams = append(v.Teams, TeamView{ID: tid, Name: t.Name, Role: tr})
			}
		}
		sort.Slice(v.Teams, func(i, j int) bool { return v.Teams[i].Name < v.Teams[j].Name })
		out.Organizations = append(out.Organizations, v)
	}
	sort.Slice(out.Organizations, func(i, j int) bool { return out.Organizations[i].Name < out.Organizations[j].Name })
	return out
}
func (d *Directory) CreateOrg(ctx context.Context, u User, name string) (string, error) {
	name, err := cleanName(name)
	if err != nil {
		return "", err
	}
	id, tid := uuid.NewString(), uuid.NewString()
	err = d.Update(ctx, func(s *State) error {
		s.Orgs[id] = Organization{ID: id, Name: name, Members: map[string]Member{u.Email: {Email: u.Email, UserID: u.ID, Role: "owner"}}, Teams: map[string]Team{tid: {ID: tid, Name: "General", Members: map[string]Member{}}}}
		audit(s, id, u.ID, "organization.created", id)
		return nil
	})
	return id, err
}
func (d *Directory) CreateTeam(ctx context.Context, u User, org, name string) (string, error) {
	name, err := cleanName(name)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	err = d.Update(ctx, func(s *State) error {
		if !IsOrgAdmin(OrgRole(*s, org, u.ID)) {
			return ErrForbidden
		}
		o := s.Orgs[org]
		o.Teams[id] = Team{ID: id, Name: name, Members: map[string]Member{}}
		s.Orgs[org] = o
		audit(s, org, u.ID, "team.created", id)
		return nil
	})
	return id, err
}
func (d *Directory) SetMember(ctx context.Context, u User, org, team, email, role string, remove bool) error {
	email, err := cleanEmail(email)
	if err != nil {
		return err
	}
	return d.Update(ctx, func(s *State) error {
		ownRole := OrgRole(*s, org, u.ID)
		o, ok := s.Orgs[org]
		if !ok {
			return ErrNotFound
		}
		if team == "" {
			if !IsOrgAdmin(ownRole) {
				return ErrForbidden
			}
			previous := o.Members[email]
			if !remove && role != "owner" && role != "admin" && role != "member" {
				return invalid("invalid organization role")
			}
			if (previous.Role == "owner" || role == "owner") && ownRole != "owner" {
				return ErrForbidden
			}
			if previous.Role == "owner" && previous.UserID != "" && (remove || role != "owner") {
				owners := 0
				for _, m := range o.Members {
					if m.Role == "owner" && m.UserID != "" {
						owners++
					}
				}
				if owners <= 1 {
					return invalid("organization must retain a verified owner")
				}
			}
			if remove {
				delete(o.Members, email)
				for id, t := range o.Teams {
					delete(t.Members, email)
					o.Teams[id] = t
				}
			} else {
				m := previous
				m.Email = email
				m.Role = role
				// New invitations bind only on a request carrying the verified email.
				// Cached profiles may refer to an old or recycled email address.
				o.Members[email] = m
			}
		} else {
			t, ok := o.Teams[team]
			if !ok {
				return ErrNotFound
			}
			_, tr := TeamRole(*s, team, u.ID)
			if tr != "admin" {
				return ErrForbidden
			}
			if !remove && role != "admin" && role != "editor" && role != "viewer" {
				return invalid("invalid team role")
			}
			if remove {
				delete(t.Members, email)
			} else {
				m, ok := o.Members[email]
				if !ok {
					return invalid("add this person to the organization first")
				}
				m.Role = role
				t.Members[email] = m
			}
			o.Teams[team] = t
		}
		s.Orgs[org] = o
		action := "membership.updated"
		if remove {
			action = "membership.removed"
		}
		audit(s, org, u.ID, action, team+":"+email+":"+role)
		return nil
	})
}

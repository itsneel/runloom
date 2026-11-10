// Package localadmin is used only by the explicit local development setup tool.
package localadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/config"
	"github.com/google/uuid"
)

// These are deliberately public development credentials, never production secrets.
const Email = "admin@runloom.test"
const Password = "RunloomLocal!123"

// ValidateKubernetes is only for the explicit local kind bootstrap Job, never
// normal API/worker startup. Require exact bundled service names before applying
// all existing local-only opt-in, URL, and database guards.
func ValidateKubernetes(environment, enabled, keycloak string, settings config.Settings, prefix string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,46}[a-z0-9]$|^[a-z]$`).MatchString(prefix) {
		return errors.New("invalid local Kubernetes service prefix")
	}
	u, err := url.Parse(keycloak)
	if err != nil || u.Hostname() != prefix+"-keycloak" {
		return errors.New("local Kubernetes bootstrap requires the bundled Keycloak service")
	}
	db, err := url.Parse(settings.DatabaseURL)
	if err != nil || db.Hostname() != prefix+"-database" {
		return errors.New("local Kubernetes bootstrap requires the bundled database service")
	}
	u.Host = strings.Replace(u.Host, prefix+"-keycloak", "keycloak", 1)
	alias := "postgres"
	if settings.DatabaseDriver == "mongo" || settings.DatabaseDriver == "mongodb" {
		alias = "mongodb"
	}
	db.Host = strings.Replace(db.Host, prefix+"-database", alias, 1)
	settings.DatabaseURL = db.String()
	return Validate(environment, enabled, u.String(), settings)
}

func Validate(environment, enabled, keycloak string, settings config.Settings) error {
	if environment != "local" || enabled != "1" {
		return errors.New("local admin setup requires RUNLOOM_ENV=local and RUNLOOM_LOCAL_BOOTSTRAP=1")
	}
	u, err := url.Parse(keycloak)
	if err != nil || u.Scheme != "http" || !localHost(u.Hostname(), "keycloak") || u.User != nil || u.Path != "/auth" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("local admin setup requires the local Keycloak /auth endpoint")
	}
	db, err := url.Parse(settings.DatabaseURL)
	if err != nil || db.Fragment != "" {
		return errors.New("invalid local database URL")
	}
	// Only permit the options needed by the bundled authenticated databases.
	// Reject arbitrary host/service overrides or ambiguous duplicate parameters.
	query, err := url.ParseQuery(db.RawQuery)
	if err != nil {
		return errors.New("invalid local database options")
	}
	for key, values := range query {
		if len(values) != 1 {
			return errors.New("invalid local database options")
		}
		mongo := settings.DatabaseDriver == "mongo" || settings.DatabaseDriver == "mongodb"
		allowed := mongo && ((key == "replicaSet" && values[0] == "runloom") || (key == "authSource" && values[0] == strings.TrimPrefix(db.Path, "/") && values[0] != ""))
		allowed = allowed || (!mongo && key == "sslmode" && values[0] == "disable")
		if !allowed {
			return errors.New("unsupported local database option")
		}
	}
	switch settings.DatabaseDriver {
	case "postgres", "postgresql":
		if (db.Scheme != "postgres" && db.Scheme != "postgresql") || !localHost(db.Hostname(), "postgres") {
			return errors.New("only local PostgreSQL is allowed")
		}
	case "mongo", "mongodb":
		if db.Scheme != "mongodb" || !localHost(db.Hostname(), "mongodb") {
			return errors.New("only local MongoDB is allowed")
		}
	default:
		return errors.New("local setup requires PostgreSQL or MongoDB; live file stores are not supported")
	}
	return nil
}

func localHost(host, service string) bool {
	return host == service || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

type keycloakUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
	Verified bool   `json:"emailVerified"`
}

// EnsureIdentity never prints provider responses or tokens and never follows redirects.
func EnsureIdentity(ctx context.Context, base string, reset bool) (access.User, bool, error) {
	return ensureIdentity(ctx, base, reset, Email, Password, "Local Admin")
}

func ensureIdentity(ctx context.Context, base string, reset bool, email, password, lastName string) (access.User, bool, error) {
	c := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"admin"}, "password": {"development-only"}}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/realms/master/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return access.User{}, false, errors.New("invalid Keycloak request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.Do(req)
	if err != nil {
		return access.User{}, false, errors.New("local Keycloak is unavailable")
	}
	defer response.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token) != nil || token.AccessToken == "" {
		return access.User{}, false, errors.New("local Keycloak bootstrap authentication failed; this tool only supports the bundled development identity provider")
	}
	call := func(method, path string, body any, output any) (int, error) {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r, err := http.NewRequestWithContext(ctx, method, base+"/admin/realms/runloom"+path, bytes.NewReader(data))
		if err != nil {
			return 0, errors.New("invalid identity request")
		}
		r.Header.Set("Authorization", "Bearer "+token.AccessToken)
		r.Header.Set("Content-Type", "application/json")
		res, err := c.Do(r)
		if err != nil {
			return 0, errors.New("local identity request failed")
		}
		defer res.Body.Close()
		if res.StatusCode >= 200 && res.StatusCode < 300 && output != nil {
			if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(output) != nil {
				return res.StatusCode, errors.New("invalid identity response")
			}
		}
		return res.StatusCode, nil
	}
	lookup := func() ([]keycloakUser, error) {
		var users []keycloakUser
		status, err := call("GET", "/users?email="+url.QueryEscape(email)+"&exact=true", nil, &users)
		if err != nil || status != 200 {
			return nil, fmt.Errorf("cannot find local identity %s", email)
		}
		return users, nil
	}
	users, err := lookup()
	if err != nil {
		return access.User{}, false, err
	}
	created := false
	if len(users) == 0 {
		status, err := call("POST", "/users", map[string]any{"username": email, "email": email, "enabled": true, "emailVerified": true, "firstName": "Runloom", "lastName": lastName, "credentials": []any{map[string]any{"type": "password", "value": password, "temporary": false}}}, nil)
		if err != nil || (status != 201 && status != 409) {
			return access.User{}, false, fmt.Errorf("cannot create local identity %s", email)
		}
		created = status == 201
		users, err = lookup()
		if err != nil {
			return access.User{}, false, err
		}
	}
	if len(users) != 1 || users[0].ID == "" || users[0].Email != email || users[0].Username != email {
		return access.User{}, false, fmt.Errorf("ambiguous or conflicting local identity %s; refusing to modify it", email)
	}
	u := users[0]
	path := "/users/" + url.PathEscape(u.ID)
	if reset {
		for _, op := range []struct {
			method, path string
			body         any
		}{
			{"PUT", path, map[string]any{"enabled": true, "emailVerified": true, "requiredActions": []string{}}},
			{"PUT", path + "/reset-password", map[string]any{"type": "password", "value": password, "temporary": false}},
			{"POST", path + "/logout", nil},
		} {
			status, err := call(op.method, op.path, op.body, nil)
			if err != nil || status != 204 {
				return access.User{}, false, errors.New("local password reset incomplete; retry local-admin-reset")
			}
		}
	} else if !u.Enabled || !u.Verified {
		return access.User{}, false, fmt.Errorf("existing local identity %s is disabled or unverified; use explicit local-admin-reset", email)
	}
	return access.User{ID: u.ID, Email: email, Name: "Runloom " + lastName}, created, nil
}

// EnsureDirectory uses the same atomic directory repository as the API. Existing
// organizations, workflows, other owners and credentials are never replaced.
func EnsureDirectory(ctx context.Context, d *access.Directory, user access.User) error {
	return ensureDirectory(ctx, d, user, Email, "owner", "admin", "local.admin.ensured", false)
}

func ensureDirectory(ctx context.Context, d *access.Directory, user access.User, email, orgRole, teamRole, event string, preserveRoles bool) error {
	if user.ID == "" || user.Email != email {
		return errors.New("invalid local account subject")
	}
	return d.Update(ctx, func(s *access.State) error {
		if s.Users == nil {
			s.Users = map[string]access.User{}
		}
		if s.Orgs == nil {
			s.Orgs = map[string]access.Organization{}
		}
		if existing, ok := s.Users[user.ID]; ok && existing.Email != email {
			return errors.New("local identity already has a different directory email")
		}
		orgID := ""
		for id, org := range s.Orgs {
			if org.Name == "Runloom" {
				if orgID != "" {
					return errors.New("multiple Runloom organizations; explicit operator selection is required")
				}
				orgID = id
			}
		}
		changed := false
		if orgID == "" {
			orgID = uuid.NewString()
			s.Orgs[orgID] = access.Organization{ID: orgID, Name: "Runloom"}
			changed = true
		}
		org := s.Orgs[orgID]
		if org.Members == nil {
			org.Members = map[string]access.Member{}
		}
		if org.Teams == nil {
			org.Teams = map[string]access.Team{}
		}
		if m := org.Members[email]; m.UserID != "" && m.UserID != user.ID {
			return errors.New("local owner is bound to a different identity; refusing to rebind")
		}
		member := access.Member{Email: email, UserID: user.ID, Role: orgRole}
		if previous, exists := org.Members[email]; preserveRoles && exists {
			member.Role = previous.Role
		}
		if org.Members[email] != member {
			org.Members[email] = member
			changed = true
		}
		teamID := ""
		for id, team := range org.Teams {
			if team.Name == "General" {
				if teamID != "" {
					return errors.New("multiple General teams; refusing to select one")
				}
				teamID = id
			}
		}
		if teamID == "" {
			teamID = uuid.NewString()
			org.Teams[teamID] = access.Team{ID: teamID, Name: "General"}
			changed = true
		}
		team := org.Teams[teamID]
		if team.Members == nil {
			team.Members = map[string]access.Member{}
		}
		if m := team.Members[email]; m.UserID != "" && m.UserID != user.ID {
			return errors.New("local team membership belongs to another identity")
		}
		member.Role = teamRole
		if previous, exists := team.Members[email]; preserveRoles && exists {
			member.Role = previous.Role
		}
		if team.Members[email] != member {
			team.Members[email] = member
			changed = true
		}
		org.Teams[teamID] = team
		s.Orgs[orgID] = org
		if _, exists := s.Users[user.ID]; !exists {
			s.Users[user.ID] = user
			changed = true
		}
		if changed {
			s.Events = append(s.Events, access.Event{OrgID: orgID, Actor: user.ID, Action: event, Target: email, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		}
		return nil
	})
}

func Result(created, reset bool) string {
	action := "Existing password preserved."
	if created {
		action = "Created with the documented local-only default password."
	}
	if reset {
		action = "Restored the documented local-only default password and ended existing sessions."
	}
	return fmt.Sprintf("Local admin ready: %s; organization Runloom; team General. %s", Email, action)
}

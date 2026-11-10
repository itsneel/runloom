package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/consumer"
	"github.com/google/uuid"
)

func accountEvent(s *access.State, a consumer.Account, actor, action string) {
	s.Events = append(s.Events, access.Event{OrgID: a.OrgID, Actor: actor, Action: action, Target: a.ClientID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}
func newClientSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (g *Gateway) serviceAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := principal(r)
	org, team := r.PathValue("org"), r.PathValue("team")
	state, _, err := g.Directory.Repo.Read(ctx)
	if err != nil {
		accessError(w, err)
		return
	}
	actualOrg, role := access.TeamRole(state, team, u.ID)
	if role != "admin" || org != actualOrg {
		accessError(w, access.ErrForbidden)
		return
	}
	if r.Method == "GET" {
		items := []consumer.Account{}
		for _, a := range state.ServiceAccounts {
			if a.TeamID == team {
				items = append(items, a)
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt < items[j].CreatedAt })
		writeJSON(w, 200, items)
		return
	}
	if g.ConsumerProvider == nil {
		writeError(w, 503, fmt.Errorf("service-account provisioning is not configured"))
		return
	}
	var policy consumer.Policy
	if r.PathValue("action") == "" {
		if err = decodeJSON(w, r, &policy); err == nil {
			err = policy.Validate()
		}
		if err != nil {
			writeError(w, 422, err)
			return
		}
		tenant, err := g.tenant(ctx, team)
		if err != nil {
			accessError(w, err)
			return
		}
		for _, grant := range policy.Grants {
			for _, version := range grant.Versions {
				d, err := tenant.backend.Get(grant.DefinitionID, &version)
				if err != nil || d.Kind == "child" {
					writeError(w, 422, fmt.Errorf("grants must reference published main workflows in this team"))
					return
				}
			}
		}
	}
	id := r.PathValue("account")
	if r.Method == "POST" && id == "" {
		secret, err := newClientSecret()
		if err != nil {
			accessError(w, err)
			return
		}
		a := consumer.Account{Policy: policy, ClientID: "runloom-sa-" + uuid.NewString(), OrgID: org, TeamID: team, Generation: uuid.NewString(), Status: "provisioning", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		err = g.Directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
			_, role := access.TeamRole(*s, team, u.ID)
			if role != "admin" {
				return access.ErrForbidden
			}
			s.ServiceAccounts[a.ClientID] = a
			accountEvent(s, a, u.ID, "service_account.created")
			return nil
		})
		if err != nil {
			accessError(w, err)
			return
		}
		providerID, subject, providerErr := g.ConsumerProvider.Create(ctx, a.ClientID, a.Generation, secret, a.Scopes)
		err = g.Directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
			current := s.ServiceAccounts[a.ClientID]
			if current.Status != "provisioning" {
				return access.ErrForbidden
			}
			current.ProviderID, current.Subject = providerID, subject
			current.Status = "active"
			if providerErr != nil {
				current.Status = "provisioning_failed"
			}
			s.ServiceAccounts[a.ClientID] = current
			a = current
			return nil
		})
		if providerErr != nil || err != nil {
			writeError(w, 503, fmt.Errorf("service account provisioning incomplete; revoke this account and create a new one"))
			return
		}
		writeJSON(w, 201, map[string]any{"account": a, "client_id": a.ClientID, "client_secret": secret})
		return
	}
	a, ok := state.ServiceAccounts[id]
	if !ok || a.TeamID != team {
		accessError(w, access.ErrNotFound)
		return
	}
	if r.Method == "PUT" {
		scopesImmutable := errors.New("scopes cannot be changed after creation; create a new service account to use different scopes")
		err = g.Directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
			current := s.ServiceAccounts[id]
			_, role := access.TeamRole(*s, team, u.ID)
			if role != "admin" || current.Status != "active" {
				return access.ErrForbidden
			}
			// Compare scope sets inside the transaction; ordering is not a permission change.
			requested, existing := slices.Clone(policy.Scopes), slices.Clone(current.Scopes)
			slices.Sort(requested)
			slices.Sort(existing)
			if !slices.Equal(slices.Compact(requested), slices.Compact(existing)) {
				return scopesImmutable
			}
			policy.Scopes = current.Scopes
			current.Policy = policy
			s.ServiceAccounts[id] = current
			accountEvent(s, current, u.ID, "service_account.policy_updated")
			return nil
		})
		if errors.Is(err, scopesImmutable) {
			writeError(w, 422, err)
			return
		}
		if err != nil {
			accessError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"updated": true})
		return
	}
	action := r.PathValue("action")
	if action != "rotate" && action != "revoke" {
		writeError(w, 404, fmt.Errorf("action not found"))
		return
	}
	generation := uuid.NewString()
	secret := ""
	if action == "rotate" {
		secret, err = newClientSecret()
		if err != nil {
			accessError(w, err)
			return
		}
	}
	err = g.Directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
		current := s.ServiceAccounts[id]
		_, role := access.TeamRole(*s, team, u.ID)
		if role != "admin" {
			return access.ErrForbidden
		}
		staleRotation := current.Status == "rotating" && time.Since(current.OperationAt) > time.Minute
		if action == "rotate" && (current.ProviderID == "" || current.Subject == "" || (current.Status != "active" && current.Status != "rotation_failed" && !staleRotation)) {
			return &access.ValidationError{Message: "account is not ready for rotation"}
		}
		current.Generation = generation
		current.OperationAt = time.Now().UTC()
		current.Status = "revoked"
		if action == "rotate" {
			current.Status = "rotating"
		}
		s.ServiceAccounts[id] = current
		a = current
		accountEvent(s, a, u.ID, "service_account."+action)
		return nil
	})
	if err != nil {
		accessError(w, err)
		return
	}
	if action == "revoke" {
		providerDisabled := true
		if a.ProviderID != "" {
			providerDisabled = g.ConsumerProvider.Disable(ctx, a.ProviderID) == nil
		}
		writeJSON(w, 200, map[string]any{"revoked": true, "provider_disabled": providerDisabled})
		return
	}
	providerErr := g.ConsumerProvider.Rotate(ctx, a.ProviderID, generation, secret, a.Scopes)
	err = g.Directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
		current := s.ServiceAccounts[id]
		if current.Status != "rotating" || current.Generation != generation {
			return access.ErrForbidden
		}
		current.Status = "active"
		if providerErr != nil {
			current.Status = "rotation_failed"
		}
		s.ServiceAccounts[id] = current
		a = current
		return nil
	})
	if err != nil || providerErr != nil {
		writeError(w, 503, fmt.Errorf("rotation incomplete; account is blocked, retry rotation"))
		return
	}
	writeJSON(w, 200, map[string]any{"account": a, "client_id": a.ClientID, "client_secret": secret})
}

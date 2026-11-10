package api

import "testing"

func TestRouteOwnership(t *testing.T) {
	for path, owner := range map[string]string{
		"/api/v1/studio/assistant/conversations": "assistant",
		"/api/v1/studio/me":                      "auth", "/api/v1/studio/orgs/o/teams/t/service-accounts": "auth",
		"/api/v1/studio/orgs/o/teams/t/secrets": "workflows", "/api/v1/studio/prompts": "management",
		"/api/v1/studio/prompts/test": "workflows", "/api/v1/studio/orgs/o/llm-analytics": "management",
		"/api/v1/studio/orgs/o/teams/t/llm-budget-policies": "management", "/api/v1/runs": "workflows",
	} {
		if got := RouteOwner(path); got != owner {
			t.Errorf("%s: got %s, want %s", path, got, owner)
		}
	}
}

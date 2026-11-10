package api

import "strings"

// RouteOwner is shared by the gateway and each destination's route firewall.
// Authorization is always performed by the destination, never by the proxy.
func RouteOwner(path string) string {
	if strings.HasPrefix(path, "/api/v1/platform/") {
		return "auth"
	}
	if path == "/api/v1/auth/config" || path == "/api/v1/studio/me" {
		return "auth"
	}
	if strings.HasPrefix(path, "/api/v1/studio/orgs") {
		for _, part := range strings.Split(path, "/") {
			if part == "secrets" || part == "connections" {
				return "workflows"
			}
			if part == "llm-analytics" || part == "llm-budget-policies" {
				return "management"
			}
		}
		return "auth"
	}
	// Testing a prompt executes a workflow and must stay in the execution domain.
	if path != "/api/v1/studio/prompts/test" && (path == "/api/v1/studio/prompts" || strings.HasPrefix(path, "/api/v1/studio/prompts/")) {
		return "management"
	}
	return "workflows"
}

func SharedRoute(path string) bool {
	return path == "/healthz" || path == "/api/v1/health" || path == "/readyz" || path == "/api/docs" || strings.HasPrefix(path, "/api/docs/")
}

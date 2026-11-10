package definition

import (
	"fmt"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

func normalizeDecision(step *model.Step, path string) error {
	if step.Decision == nil {
		return fmt.Errorf("%s.decision is required", path)
	}
	c := *step.Decision
	if c.Mode != "single" && c.Mode != "multiple" && c.Mode != "agent" {
		return fmt.Errorf("%s.decision.mode must be single, multiple or agent", path)
	}
	if c.Execution == "" {
		c.Execution = "sequential"
	}
	if c.Execution != "sequential" && c.Execution != "parallel" {
		return fmt.Errorf("%s.decision.execution must be sequential or parallel", path)
	}
	if c.Mode != "multiple" && c.Execution != "sequential" {
		return fmt.Errorf("%s: only multiple mode supports parallel actions", path)
	}
	if c.MaxRounds == 0 {
		c.MaxRounds = 1
		if c.Mode == "agent" {
			c.MaxRounds = 5
		}
	}
	if c.MaxRounds < 1 || c.MaxRounds > 20 || (c.Mode != "agent" && c.MaxRounds != 1) {
		return fmt.Errorf("%s: max_rounds must be 1, or 1–20 for agent mode", path)
	}
	if c.MaxActions == 0 {
		c.MaxActions = 1
		if c.Mode != "single" {
			c.MaxActions = 10
		}
	}
	if c.MaxActions < 1 || c.MaxActions > 50 || (c.Mode == "single" && c.MaxActions != 1) {
		return fmt.Errorf("%s: max_actions must be 1, or 1–50 for multiple/agent mode", path)
	}
	if len(step.Branches) < 1 || len(step.Branches) > 20 {
		return fmt.Errorf("%s: define 1–20 available actions", path)
	}
	ids := map[string]bool{}
	for _, b := range step.Branches {
		if !idPattern.MatchString(b.ID) || ids[b.ID] {
			return fmt.Errorf("%s: action IDs must be valid and unique", path)
		}
		ids[b.ID] = true
		if strings.TrimSpace(b.Description) == "" || len(b.Description) > 2000 || len(b.Steps) == 0 {
			return fmt.Errorf("%s: every action needs a description and at least one node", path)
		}
	}
	// Reuse the existing LLM validation without changing legacy LLM defaults.
	llm := *step
	llm.Type, llm.Decision, llm.Branches = "llm", nil, nil
	if llm.Retry == nil {
		llm.Retry = &model.RetryPolicy{MaximumAttempts: 1}
	}
	count := 0
	normalized, err := normalizeSteps([]model.Step{llm}, map[string]bool{}, map[string]bool{}, &count, 1, path)
	if err != nil {
		return err
	}
	step.Temperature, step.MaxTokens, step.TimeoutSeconds, step.Retry = normalized[0].Temperature, normalized[0].MaxTokens, normalized[0].TimeoutSeconds, normalized[0].Retry
	if step.Retry.MaximumAttempts > 3 {
		return fmt.Errorf("%s: AI decisions allow at most 3 provider attempts per round", path)
	}
	step.Decision = &c
	return nil
}

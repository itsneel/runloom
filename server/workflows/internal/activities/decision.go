package activities

import (
	"encoding/json"
	"fmt"
)

type decisionCall struct {
	Mode      string `json:"mode"`
	Remaining int    `json:"remaining"`
	Actions   []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	} `json:"actions"`
	History any `json:"history"`
}

func parseDecisionCall(raw any) (*decisionCall, error) {
	data, err := json.Marshal(raw)
	if err != nil || len(data) > 256*1024 {
		return nil, fmt.Errorf("decision context exceeds 256 KiB")
	}
	var c decisionCall
	if json.Unmarshal(data, &c) != nil || (c.Mode != "single" && c.Mode != "multiple" && c.Mode != "agent") || c.Remaining < 0 || c.Remaining > 50 || len(c.Actions) == 0 || len(c.Actions) > 20 {
		return nil, fmt.Errorf("invalid decision configuration")
	}
	seen := map[string]bool{}
	for _, a := range c.Actions {
		if a.ID == "" || len(a.ID) > 64 || seen[a.ID] || a.Description == "" || len(a.Description) > 2000 {
			return nil, fmt.Errorf("invalid decision action catalogue")
		}
		seen[a.ID] = true
	}
	return &c, nil
}
func (c *decisionCall) ids() []string {
	ids := make([]string, len(c.Actions))
	for i, a := range c.Actions {
		ids[i] = a.ID
	}
	return ids
}

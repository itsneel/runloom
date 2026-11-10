package definition

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

func validateBuildingNode(step model.Step) error {
	allowed := map[string]bool{"id": true, "name": true, "type": true, "output_schema": true}
	switch step.Type {
	case "integration":
		allowed["integration"] = true
		if err := ValidateIntegration(step.Integration, false); err != nil {
			return err
		}
	case "transform", "collection", "merge":
		allowed["data"] = true
		if err := ValidateData(step.Data, step.Type); err != nil {
			return err
		}
	case "foreach":
		allowed["foreach"], allowed["branches"] = true, true
		if step.ForEach == nil || step.ForEach.Items == nil {
			return fmt.Errorf("For Each requires items")
		}
		if step.ForEach.Concurrency < 1 || step.ForEach.Concurrency > 10 {
			return fmt.Errorf("For Each concurrency must be 1 to 10")
		}
		if len(step.Branches) != 1 || step.Branches[0].ID != "body" || len(step.Branches[0].Steps) == 0 {
			return fmt.Errorf("For Each requires one nonempty body branch named body")
		}
	case "fail":
		allowed["message"] = true
		if strings.TrimSpace(step.Message) == "" || len(step.Message) > 2000 {
			return fmt.Errorf("failure message is required and must be at most 2000 characters")
		}
	}
	raw, err := json.Marshal(step)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for key := range fields {
		if !allowed[key] {
			return fmt.Errorf("%s does not support field %s", step.Type, key)
		}
	}
	return nil
}

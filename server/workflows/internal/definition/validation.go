package definition

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/example/runloom/workflows/internal/mailnode"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	MaxDefinitionBytes = 1024 * 1024
	MaxSteps           = 500
	MaxDepth           = 10
)

var (
	idPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	activityPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,127}$`)
	operators       = map[string]bool{
		"equals": true, "not_equals": true, "contains": true, "exists": true,
		"greater_than": true, "greater_than_or_equal": true,
		"less_than": true, "less_than_or_equal": true,
	}
)

func Normalize(value model.Definition) (model.Definition, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value, fmt.Errorf("definition must contain only JSON values: %w", err)
	}
	if len(encoded) > MaxDefinitionBytes {
		return value, fmt.Errorf("definition exceeds the 1 MiB limit")
	}
	if value.SchemaVersion != "1.0" {
		return value, fmt.Errorf("schema_version must be 1.0")
	}
	if !idPattern.MatchString(value.ID) {
		return value, fmt.Errorf("definition.id is invalid")
	}
	if len(value.Name) < 1 || len(value.Name) > 120 {
		return value, fmt.Errorf("name is required and must be at most 120 characters")
	}
	if len(value.Description) > 2000 {
		return value, fmt.Errorf("description must be at most 2000 characters")
	}
	if value.Version < 0 {
		return value, fmt.Errorf("version cannot be negative")
	}
	if value.Kind == "" {
		value.Kind = "main"
	}
	if value.Kind != "main" && value.Kind != "child" {
		return value, fmt.Errorf("kind must be main or child")
	}
	if value.Kind == "child" && value.Trigger.Type == "webhook" {
		return value, fmt.Errorf("child workflows cannot have webhook triggers")
	}
	if value.Trigger.Type == "" {
		value.Trigger = model.Trigger{Type: "manual"}
	}
	switch value.Trigger.Type {
	case "manual":
		if value.Trigger.Path != "" || value.Trigger.Method != "" {
			return value, fmt.Errorf("manual trigger contains unsupported fields")
		}
	case "webhook":
		if !idPattern.MatchString(value.Trigger.Path) {
			return value, fmt.Errorf("trigger.path is invalid")
		}
		if value.Trigger.Method != "POST" {
			return value, fmt.Errorf("trigger.method must be POST")
		}
	default:
		return value, fmt.Errorf("trigger.type must be manual or webhook")
	}
	if value.InputSchema == nil {
		value.InputSchema = map[string]any{}
	}
	if err := rejectRemoteReferences(value.InputSchema); err != nil {
		return value, err
	}
	if _, err := compileInputSchema(value.InputSchema); err != nil {
		return value, fmt.Errorf("input_schema is invalid: %w", err)
	}
	if len(value.Steps) == 0 {
		return value, fmt.Errorf("steps must be a non-empty array")
	}
	seen := map[string]bool{}
	seenSignals := map[string]bool{}
	count := 0
	if len(value.SignalHandlers) > 20 {
		return value, fmt.Errorf("at most 20 signal handlers are supported")
	}
	for _, h := range value.SignalHandlers {
		if !idPattern.MatchString(h.ID) || seen[h.ID] {
			return value, fmt.Errorf("signal handler id is invalid or duplicated")
		}
		if !activityPattern.MatchString(h.SignalName) || seenSignals[h.SignalName] || h.SignalName == "signal" || h.SignalName == "approval" || h.SignalName == "callback" || h.SignalName == "resume" {
			return value, fmt.Errorf("signal handler name is invalid, reserved or duplicated")
		}
		if len(h.Name) > 120 || len(h.Steps) == 0 {
			return value, fmt.Errorf("signal handlers require a nonempty path and a name of at most 120 characters")
		}
		if h.InputSchema != nil {
			if err := rejectRemoteReferences(h.InputSchema); err != nil {
				return value, err
			}
			if _, err := compileInputSchema(h.InputSchema); err != nil {
				return value, fmt.Errorf("signal handler input_schema is invalid: %w", err)
			}
		}
		seen[h.ID], seenSignals[h.SignalName] = true, true
		count++
	}
	steps, err := normalizeSteps(value.Steps, seen, seenSignals, &count, 1, "steps")
	if err != nil {
		return value, err
	}
	value.Steps = steps
	for i := range value.SignalHandlers {
		value.SignalHandlers[i].Steps, err = normalizeSteps(value.SignalHandlers[i].Steps, seen, seenSignals, &count, 1, fmt.Sprintf("signal_handlers[%d].steps", i))
		if err != nil {
			return value, err
		}
	}
	if err := ValidatePromptMappings(value); err != nil {
		return value, err
	}
	return value, validateLayout(value.Layout)
}

func validateLayout(layout *model.CanvasLayout) error {
	if layout != nil {
		if layout.Positions == nil || len(layout.Positions) > 1002 {
			return fmt.Errorf("layout.positions must be an object with at most 1002 entries")
		}
		for key, point := range layout.Positions {
			id := key
			for _, prefix := range []string{"@join:", "@handler:", "@return:"} {
				if strings.HasPrefix(key, prefix) {
					id = strings.TrimPrefix(key, prefix)
					break
				}
			}
			if key != "@trigger" && key != "@finish" && !idPattern.MatchString(id) {
				return fmt.Errorf("layout position key is invalid")
			}
			if math.IsNaN(point.X) || math.IsNaN(point.Y) || math.IsInf(point.X, 0) || math.IsInf(point.Y, 0) || math.Abs(point.X) > 100000 || math.Abs(point.Y) > 100000 {
				return fmt.Errorf("layout coordinates must be finite numbers between -100000 and 100000")
			}
		}
	}
	return nil
}

func ValidateInput(schemaValue map[string]any, input any) error {
	schema, err := compileInputSchema(schemaValue)
	if err != nil {
		return fmt.Errorf("input_schema is invalid: %w", err)
	}
	if err := schema.Validate(input); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	return nil
}

func compileInputSchema(value map[string]any) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	const location = "urn:runloom:input-schema"
	if err := compiler.AddResource(location, value); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

func rejectRemoteReferences(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		if reference, ok := typed["$ref"].(string); ok && (len(reference) == 0 || reference[0] != '#') {
			return fmt.Errorf("input_schema cannot contain remote $ref values")
		}
		for _, child := range typed {
			if err := rejectRemoteReferences(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectRemoteReferences(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeSteps(steps []model.Step, seen map[string]bool, seenSignals map[string]bool, count *int, depth int, path string) ([]model.Step, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("%s exceeds the maximum nesting depth of %d", path, MaxDepth)
	}
	result := make([]model.Step, len(steps))
	for index, step := range steps {
		stepPath := fmt.Sprintf("%s[%d]", path, index)
		var err error
		step, err = normalizeStep(step, index, len(steps), seen, seenSignals, count, depth, path, stepPath, true)
		if err != nil {
			return nil, err
		}
		result[index] = step
	}
	return result, nil
}

// normalizeStep is shared by strict normalization and non-mutating editor diagnostics.
func normalizeStep(step model.Step, index, length int, seen, seenSignals map[string]bool, count *int, depth int, path, stepPath string, descend bool) (model.Step, error) {
	if !idPattern.MatchString(step.ID) {
		return step, fmt.Errorf("%s.id is invalid", stepPath)
	}
	if seen[step.ID] {
		return step, fmt.Errorf("duplicate step id: %s", step.ID)
	}
	seen[step.ID] = true
	*count = *count + 1
	if *count > MaxSteps {
		return step, fmt.Errorf("definition cannot exceed %d total steps", MaxSteps)
	}
	if len(step.Name) > 120 {
		return step, fmt.Errorf("%s.name is invalid", stepPath)
	}
	if err := validatePromptStep(step); err != nil {
		return step, fmt.Errorf("%s: %w", stepPath, err)
	}
	if step.OutputSchema != nil {
		if err := rejectRemoteReferences(step.OutputSchema); err != nil {
			return step, err
		}
		if _, err := compileInputSchema(step.OutputSchema); err != nil {
			return step, fmt.Errorf("invalid output_schema: %w", err)
		}
	}
	if step.Type != "subworkflow" && (step.DefinitionID != "" || step.Version != 0) {
		return step, fmt.Errorf("%s contains subworkflow fields on a different node type", stepPath)
	}
	if step.Type != "http" && step.HTTP != nil {
		return step, fmt.Errorf("%s contains HTTP fields on another node type", stepPath)
	}
	if step.Type != "database" && step.Database != nil {
		return step, fmt.Errorf("%s contains database fields on another node type", stepPath)
	}
	if step.Type != "code" && step.Code != nil {
		return step, fmt.Errorf("%s contains code on another node type", stepPath)
	}
	if step.Type != "email" && step.Email != nil {
		return step, fmt.Errorf("%s contains email on another node type", stepPath)
	}
	if step.Type != "decision" && step.Decision != nil {
		return step, fmt.Errorf("%s contains decision settings on another node type", stepPath)
	}
	if step.Data != nil && step.Type != "transform" && step.Type != "collection" && step.Type != "merge" {
		return step, fmt.Errorf("%s contains data settings on another node type", stepPath)
	}
	if step.ForEach != nil && step.Type != "foreach" {
		return step, fmt.Errorf("%s contains foreach settings on another node type", stepPath)
	}
	if step.LLMTask != nil {
		if step.Type != "llm" {
			return step, fmt.Errorf("%s: llm_task requires an LLM node", stepPath)
		}
		if err := step.LLMTask.Validate(); err != nil {
			return step, fmt.Errorf("%s: %w", stepPath, err)
		}
	}
	if step.Integration != nil && step.Type != "integration" {
		return step, fmt.Errorf("%s: integration configuration requires an Integration node", stepPath)
	}
	switch step.Type {
	case "transform", "collection", "merge", "foreach", "fail", "integration":
		if err := validateBuildingNode(step); err != nil {
			return step, fmt.Errorf("%s: %w", stepPath, err)
		}
		if step.Type == "fail" && index != length-1 {
			return step, fmt.Errorf("%s: Fail must be last in its path", stepPath)
		}
		if step.Type == "foreach" {
			if strings.Contains(path, ".foreach_body") {
				return step, fmt.Errorf("%s: nested For Each is not supported; use a child workflow", stepPath)
			}
			if descend {
				var err error
				step.Branches[0].Steps, err = normalizeSteps(step.Branches[0].Steps, seen, seenSignals, count, depth+1, stepPath+".foreach_body.branches[0].steps")
				if err != nil {
					return step, err
				}
			}
		}
	case "database":
		if err := dbnode.Validate(step.Database); err != nil {
			return step, fmt.Errorf("%s: %w", stepPath, err)
		}
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.HTTP != nil || step.Email != nil || step.Code != nil || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 || step.TimeoutSeconds != nil || step.Retry != nil {
			return step, fmt.Errorf("%s: database nodes use one attempt and a fixed timeout; unrelated fields are not supported", stepPath)
		}
	case "decision":
		if strings.Contains(path, ".decision_actions") {
			return step, fmt.Errorf("%s: nested AI Decision nodes are not allowed; call a bounded child workflow instead", stepPath)
		}
		if err := normalizeDecision(&step, stepPath); err != nil {
			return step, err
		}
		for i := range step.Branches {
			var err error
			if descend {
				step.Branches[i].Steps, err = normalizeSteps(step.Branches[i].Steps, seen, seenSignals, count, depth+1, fmt.Sprintf("%s.decision_actions.branches[%d].steps", stepPath, i))
			}
			if err != nil {
				return step, err
			}
		}
	case "email":
		if err := mailnode.Validate(step.Email, true); err != nil {
			return step, fmt.Errorf("%s: %w", stepPath, err)
		}
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 || step.TimeoutSeconds != nil || step.Retry != nil {
			return step, fmt.Errorf("%s contains unsupported email fields; email uses one attempt and a fixed timeout", stepPath)
		}
	case "code":
		if err := codecontract.Validate(step.Code); err != nil {
			return step, fmt.Errorf("%s: %w", stepPath, err)
		}
		if step.Activity != "" || hasLLMFields(step) || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 || step.TimeoutSeconds != nil || step.Retry != nil {
			return step, fmt.Errorf("%s contains unsupported code fields; timeout and retries are managed by the runtime", stepPath)
		}
		if step.With == nil {
			step.With = map[string]any{}
		}
		if err := codecontract.ValidateInput(model.CodeActivityInput{Code: *step.Code, Input: step.With}); err != nil {
			return step, fmt.Errorf("%s: %w", stepPath, err)
		}
	case "http", "end":
		if step.Activity != "" || hasLLMFields(step) || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains unsupported fields", stepPath)
		}
		if step.Type == "end" {
			if step.TimeoutSeconds != nil || step.Retry != nil || len(step.Message) > 1000 {
				return step, fmt.Errorf("%s contains unsupported End fields", stepPath)
			}
			if strings.Contains(path, ".branches[") {
				return step, fmt.Errorf("%s: place End after the parallel join, not inside a parallel branch", stepPath)
			}
			if index != length-1 {
				return step, fmt.Errorf("%s: steps after End are unreachable; move End to the end of this path", stepPath)
			}
		} else {
			if step.With != nil || step.Message != "" {
				return step, fmt.Errorf("%s contains unsupported HTTP fields", stepPath)
			}
			if err := ValidateHTTP(step.HTTP); err != nil {
				return step, fmt.Errorf("%s: %w", stepPath, err)
			}
			if step.TimeoutSeconds == nil {
				timeout := 30
				step.TimeoutSeconds = &timeout
			}
			if *step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 300 {
				return step, fmt.Errorf("%s.timeout_seconds must be 1 to 300", stepPath)
			}
			if step.Retry == nil {
				step.Retry = &model.RetryPolicy{MaximumAttempts: 1}
			}
			if step.Retry.MaximumAttempts == 0 {
				step.Retry.MaximumAttempts = 1
			}
			if err := normalizeRetry(&step, stepPath); err != nil {
				return step, err
			}
		}
	case "subworkflow":
		if !idPattern.MatchString(step.DefinitionID) || step.Version < 1 {
			return step, fmt.Errorf("%s requires definition_id and a published version >= 1", stepPath)
		}
		if step.Activity != "" || hasLLMFields(step) || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 || step.Retry != nil {
			return step, fmt.Errorf("%s contains fields unsupported by subworkflow steps", stepPath)
		}
		if step.With == nil {
			step.With = map[string]any{}
		}
		if step.TimeoutSeconds == nil {
			timeout := 3600
			step.TimeoutSeconds = &timeout
		}
		if *step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 31536000 {
			return step, fmt.Errorf("%s.timeout_seconds must be between 1 and 31536000", stepPath)
		}
	case "activity":
		if strings.HasPrefix(step.Activity, "runloom.internal.") {
			return step, fmt.Errorf("%s cannot call internal runtime activities", stepPath)
		}
		if hasLLMFields(step) || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by activity steps", stepPath)
		}
		if !activityPattern.MatchString(step.Activity) {
			return step, fmt.Errorf("%s.activity is invalid", stepPath)
		}
		if step.With == nil {
			step.With = map[string]any{}
		}
		if step.TimeoutSeconds == nil {
			value := 60
			step.TimeoutSeconds = &value
		}
		if *step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 86400 {
			return step, fmt.Errorf("%s.timeout_seconds must be between 1 and 86400", stepPath)
		}
		if err := normalizeRetry(&step, stepPath); err != nil {
			return step, err
		}
	case "llm":
		if step.Activity != "" || step.With != nil || step.Seconds != 0 || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by llm steps", stepPath)
		}
		if len(step.Model) < 1 || len(step.Model) > 200 {
			return step, fmt.Errorf("%s.model is required", stepPath)
		}
		if step.PromptTemplate == nil && (len(step.SystemPrompt) > 20000 || len(step.Prompt) < 1 || len(step.Prompt) > 100000) {
			return step, fmt.Errorf("%s prompts are invalid", stepPath)
		}
		if step.Temperature == nil {
			value := 0.2
			step.Temperature = &value
		}
		if *step.Temperature < 0 || *step.Temperature > 2 {
			return step, fmt.Errorf("%s.temperature must be between 0 and 2", stepPath)
		}
		if step.MaxTokens == nil {
			value := 2000
			step.MaxTokens = &value
		}
		if *step.MaxTokens < 1 || *step.MaxTokens > 128000 {
			return step, fmt.Errorf("%s.max_tokens is invalid", stepPath)
		}
		if step.TimeoutSeconds == nil {
			value := 120
			step.TimeoutSeconds = &value
		}
		if *step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 1800 {
			return step, fmt.Errorf("%s.timeout_seconds is invalid", stepPath)
		}
		if err := normalizeRetry(&step, stepPath); err != nil {
			return step, err
		}
	case "wait":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.TimeoutSeconds != nil || step.Retry != nil || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by wait steps", stepPath)
		}
		if step.Seconds < 1 || step.Seconds > 31536000 {
			return step, fmt.Errorf("%s.seconds must be between 1 and 31536000", stepPath)
		}
	case "approval":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.Retry != nil || step.Seconds != 0 || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by approval steps", stepPath)
		}
		if len(step.Prompt) < 1 || len(step.Prompt) > 1000 {
			return step, fmt.Errorf("%s.prompt is required", stepPath)
		}
		if step.TimeoutSeconds != nil && (*step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 31536000) {
			return step, fmt.Errorf("%s.timeout_seconds is invalid", stepPath)
		}
		if step.OnTimeout == "" {
			step.OnTimeout = "reject"
		}
		if step.OnTimeout != "reject" && step.OnTimeout != "fail" {
			return step, fmt.Errorf("%s.on_timeout is invalid", stepPath)
		}
	case "callback":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.Retry != nil || step.Seconds != 0 || step.Message != "" || step.SignalName != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by callback steps", stepPath)
		}
		if !activityPattern.MatchString(step.CallbackKey) {
			return step, fmt.Errorf("%s.callback_key is invalid", stepPath)
		}
		if len(step.Prompt) < 1 || len(step.Prompt) > 1000 {
			return step, fmt.Errorf("%s.prompt is required", stepPath)
		}
		if step.TimeoutSeconds != nil && (*step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 31536000) {
			return step, fmt.Errorf("%s.timeout_seconds is invalid", stepPath)
		}
		if step.OnTimeout == "" {
			step.OnTimeout = "fail"
		}
		if step.OnTimeout != "fail" && step.OnTimeout != "continue" {
			return step, fmt.Errorf("%s.on_timeout is invalid", stepPath)
		}
	case "pause":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.Retry != nil || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.SignalName != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by pause steps", stepPath)
		}
		if len(step.Message) < 1 || len(step.Message) > 1000 {
			return step, fmt.Errorf("%s.message is required", stepPath)
		}
		if step.TimeoutSeconds != nil && (*step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 31536000) {
			return step, fmt.Errorf("%s.timeout_seconds is invalid", stepPath)
		}
		if step.OnTimeout == "" {
			step.OnTimeout = "fail"
		}
		if step.OnTimeout != "fail" && step.OnTimeout != "resume" {
			return step, fmt.Errorf("%s.on_timeout is invalid", stepPath)
		}
	case "signal":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.Retry != nil || step.Seconds != 0 || step.CallbackKey != "" || step.Message != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by signal steps", stepPath)
		}
		if !activityPattern.MatchString(step.SignalName) {
			return step, fmt.Errorf("%s.signal_name is invalid", stepPath)
		}
		if seenSignals[step.SignalName] {
			return step, fmt.Errorf("duplicate signal name: %s", step.SignalName)
		}
		seenSignals[step.SignalName] = true
		if len(step.Prompt) < 1 || len(step.Prompt) > 1000 {
			return step, fmt.Errorf("%s.prompt is required", stepPath)
		}
		if step.TimeoutSeconds != nil && (*step.TimeoutSeconds < 1 || *step.TimeoutSeconds > 31536000) {
			return step, fmt.Errorf("%s.timeout_seconds is invalid", stepPath)
		}
		if step.OnTimeout == "" {
			step.OnTimeout = "fail"
		}
		if step.OnTimeout != "fail" && step.OnTimeout != "continue" {
			return step, fmt.Errorf("%s.on_timeout is invalid", stepPath)
		}
	case "condition":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.TimeoutSeconds != nil || step.Retry != nil || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || len(step.Branches) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by condition steps", stepPath)
		}
		if step.If == nil || !operators[step.If.Operator] {
			return step, fmt.Errorf("%s.if is invalid", stepPath)
		}
		if len(step.Then) == 0 {
			return step, fmt.Errorf("%s.then must be a non-empty array", stepPath)
		}
		var err error
		if descend {
			step.Then, err = normalizeSteps(step.Then, seen, seenSignals, count, depth+1, stepPath+".then")
		}
		if err != nil {
			return step, err
		}
		if descend {
			step.Else, err = normalizeSteps(step.Else, seen, seenSignals, count, depth+1, stepPath+".else")
		}
		if err != nil {
			return step, err
		}
	case "parallel":
		if step.Activity != "" || step.With != nil || hasLLMFields(step) || step.TimeoutSeconds != nil || step.Retry != nil || step.Seconds != 0 || step.Prompt != "" || step.CallbackKey != "" || step.Message != "" || step.SignalName != "" || step.OnTimeout != "" || step.If != nil || len(step.Then) != 0 || len(step.Else) != 0 {
			return step, fmt.Errorf("%s contains fields unsupported by parallel steps", stepPath)
		}
		if len(step.Branches) < 2 || len(step.Branches) > 50 {
			return step, fmt.Errorf("%s.branches must contain 2 to 50 branches", stepPath)
		}
		branchIDs := map[string]bool{}
		for branchIndex := range step.Branches {
			branch := &step.Branches[branchIndex]
			if !idPattern.MatchString(branch.ID) || branchIDs[branch.ID] {
				return step, fmt.Errorf("%s.branches[%d].id is invalid or duplicated", stepPath, branchIndex)
			}
			branchIDs[branch.ID] = true
			if len(branch.Steps) == 0 {
				return step, fmt.Errorf("%s.branches[%d].steps must be non-empty", stepPath, branchIndex)
			}
			var err error
			if descend {
				branch.Steps, err = normalizeSteps(branch.Steps, seen, seenSignals, count, depth+1, fmt.Sprintf("%s.branches[%d].steps", stepPath, branchIndex))
			}
			if err != nil {
				return step, err
			}
		}
	default:
		return step, fmt.Errorf("%s.type is unsupported", stepPath)
	}
	return step, nil
}

func hasLLMFields(step model.Step) bool {
	return step.Model != "" || step.SystemPrompt != "" || step.Temperature != nil || step.MaxTokens != nil
}

func normalizeRetry(step *model.Step, stepPath string) error {
	if step.Retry == nil {
		step.Retry = &model.RetryPolicy{}
	}
	if step.Retry.MaximumAttempts == 0 {
		step.Retry.MaximumAttempts = 3
	}
	if step.Retry.InitialInterval == 0 {
		step.Retry.InitialInterval = 1
	}
	if step.Retry.BackoffCoefficient == 0 {
		step.Retry.BackoffCoefficient = 2
	}
	if step.Retry.MaximumInterval == 0 {
		step.Retry.MaximumInterval = 60
	}
	if step.Retry.MaximumAttempts < 1 || step.Retry.MaximumAttempts > 100 || step.Retry.InitialInterval <= 0 || step.Retry.InitialInterval > 86400 || step.Retry.BackoffCoefficient < 1 || step.Retry.BackoffCoefficient > 100 || step.Retry.MaximumInterval <= 0 || step.Retry.MaximumInterval > 86400 {
		return fmt.Errorf("%s.retry is invalid", stepPath)
	}
	return nil
}

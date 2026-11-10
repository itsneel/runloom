package definition

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

var referencePattern = regexp.MustCompile(`\$\{([a-zA-Z0-9_.-]+)\}`)

type Context struct {
	Signal   map[string]any
	Loop     map[string]any
	Decision map[string]any
	Input    map[string]any
	Steps    map[string]any
}

func (c Context) Value() map[string]any {
	value := map[string]any{"input": c.Input, "steps": c.Steps, "decision": c.Decision}
	if c.Signal != nil {
		value["signal"] = c.Signal
	}
	if c.Loop != nil {
		value["loop"] = c.Loop
	}
	return value
}

func Lookup(path string, context map[string]any) (any, error) {
	var current any = context
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("value reference not found: %s", path)
		}
		current, ok = object[segment]
		if !ok {
			return nil, fmt.Errorf("value reference not found: %s", path)
		}
	}
	return current, nil
}

func Resolve(value any, context map[string]any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		//workflowcheck:ignore map keys are sorted before any workflow command can use them
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		resolved := make(map[string]any, len(typed))
		for _, key := range keys {
			item, err := Resolve(typed[key], context)
			if err != nil {
				return nil, err
			}
			resolved[key] = item
		}
		return resolved, nil
	case []any:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			var err error
			resolved[index], err = Resolve(item, context)
			if err != nil {
				return nil, err
			}
		}
		return resolved, nil
	case string:
		return resolveString(typed, context)
	default:
		return value, nil
	}
}

func resolveString(value string, context map[string]any) (any, error) {
	complete := referencePattern.FindStringSubmatch(value)
	if len(complete) == 2 && complete[0] == value {
		return Lookup(complete[1], context)
	}
	var firstErr error
	resolved := referencePattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := referencePattern.FindStringSubmatch(match)
		item, err := Lookup(parts[1], context)
		if err != nil {
			firstErr = err
			return match
		}
		switch typed := item.(type) {
		case nil:
			return "null"
		case bool:
			return strconv.FormatBool(typed)
		case string:
			return typed
		default:
			return stableString(typed)
		}
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return resolved, nil
}

func Evaluate(expression *model.ConditionExpression, context map[string]any) (bool, error) {
	left, err := Resolve(expression.Left, context)
	exists := err == nil && left != nil
	if expression.Operator == "exists" {
		return exists, nil
	}
	if err != nil {
		return false, err
	}
	right, err := Resolve(expression.Right, context)
	if err != nil {
		return false, err
	}
	switch expression.Operator {
	case "equals":
		return equal(left, right), nil
	case "not_equals":
		return !equal(left, right), nil
	case "contains":
		return contains(left, right), nil
	case "greater_than", "greater_than_or_equal", "less_than", "less_than_or_equal":
		leftNumber, leftOK := number(left)
		rightNumber, rightOK := number(right)
		if !leftOK || !rightOK {
			return false, fmt.Errorf("operator %s requires numeric values", expression.Operator)
		}
		switch expression.Operator {
		case "greater_than":
			return leftNumber > rightNumber, nil
		case "greater_than_or_equal":
			return leftNumber >= rightNumber, nil
		case "less_than":
			return leftNumber < rightNumber, nil
		default:
			return leftNumber <= rightNumber, nil
		}
	default:
		return false, fmt.Errorf("unsupported operator: %s", expression.Operator)
	}
}

func contains(left, right any) bool {
	switch typed := left.(type) {
	case string:
		value, ok := right.(string)
		return ok && strings.Contains(typed, value)
	case []any:
		for _, item := range typed {
			if equal(item, right) {
				return true
			}
		}
	case map[string]any:
		key, ok := right.(string)
		if ok {
			_, found := typed[key]
			return found
		}
	}
	return false
}

func equal(left, right any) bool {
	if leftNumber, ok := number(left); ok {
		rightNumber, rightOK := number(right)
		return rightOK && leftNumber == rightNumber
	}
	switch typed := left.(type) {
	case nil:
		return right == nil
	case bool:
		value, ok := right.(bool)
		return ok && typed == value
	case string:
		value, ok := right.(string)
		return ok && typed == value
	case []any:
		value, ok := right.([]any)
		if !ok || len(typed) != len(value) {
			return false
		}
		for index := range typed {
			if !equal(typed[index], value[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		value, ok := right.(map[string]any)
		if !ok || len(typed) != len(value) {
			return false
		}
		keys := make([]string, 0, len(typed))
		//workflowcheck:ignore map keys are sorted before comparison
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			rightItem, found := value[key]
			if !found || !equal(typed[key], rightItem) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func stableString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(typed)
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case []any:
		parts := make([]string, len(typed))
		for index, item := range typed {
			parts[index] = jsonValue(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(typed))
		//workflowcheck:ignore map keys are sorted before formatting
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for index, key := range keys {
			parts[index] = strconv.Quote(key) + ":" + jsonValue(typed[key])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		return ""
	}
}

func jsonValue(value any) string {
	if text, ok := value.(string); ok {
		return strconv.Quote(text)
	}
	return stableString(value)
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

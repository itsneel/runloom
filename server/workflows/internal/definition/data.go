package definition

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

const MaxDataItems = 1000
const MaxDataBytes = 256 * 1024

func ValidateData(d *model.DataOperation, kind string) error {
	if d == nil {
		return fmt.Errorf("data configuration is required")
	}
	allowed := map[string][]string{
		"transform":  {"map"},
		"collection": {"filter", "sort", "limit", "unique", "count", "sum", "split", "compare"},
		"merge":      {"append", "join"},
	}
	ok := false
	for _, op := range allowed[kind] {
		if d.Operation == op {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("unsupported %s operation", kind)
	}
	if d.Limit < 0 || d.Limit > MaxDataItems {
		return fmt.Errorf("limit must be between 0 and %d", MaxDataItems)
	}
	if d.Operation != "map" && (len(d.Fields) > 0 || d.KeepSource) {
		return fmt.Errorf("fields and keep_source require map")
	}
	if d.Operation != "filter" && d.Predicate != nil {
		return fmt.Errorf("predicate requires filter")
	}
	if d.Operation == "filter" && (d.Predicate == nil || !operators[d.Predicate.Operator]) {
		return fmt.Errorf("filter requires a valid predicate")
	}
	if d.Operation != "sort" && d.Descending {
		return fmt.Errorf("descending requires sort")
	}
	if d.Operation != "limit" && d.Limit != 0 {
		return fmt.Errorf("limit requires limit operation")
	}
	if d.Operation != "join" && d.OtherKey != "" {
		return fmt.Errorf("other_key requires join")
	}
	if d.Operation != "join" && d.Operation != "append" && d.Operation != "compare" && d.Other != nil {
		return fmt.Errorf("other requires append, join or compare")
	}
	if (d.Operation == "join" || d.Operation == "sort" || d.Operation == "sum" || d.Operation == "split") && strings.TrimSpace(d.Key) == "" {
		return fmt.Errorf("key is required")
	}
	if d.Operation == "join" && strings.TrimSpace(d.OtherKey) == "" {
		return fmt.Errorf("other_key is required")
	}
	if len(d.Key) > 256 || len(d.OtherKey) > 256 {
		return fmt.Errorf("field path is too long")
	}
	if len(d.Fields) > 100 {
		return fmt.Errorf("at most 100 field mappings are allowed")
	}
	names := map[string]bool{}
	for _, f := range d.Fields {
		if !idPattern.MatchString(f.Name) || names[f.Name] {
			return fmt.Errorf("field names must be unique lowercase identifiers")
		}
		names[f.Name] = true
		switch f.Type {
		case "", "string", "number", "integer", "boolean", "object", "array":
		default:
			return fmt.Errorf("unsupported field conversion")
		}
	}
	return nil
}

// DataResultBound is applied before returning data to workflow state.
func DataResultBound(value any) (any, error) {
	raw, err := marshalData(value)
	if err != nil || len(raw) > MaxDataBytes {
		return nil, fmt.Errorf("data result must be JSON and at most 256 KiB")
	}
	return value, nil
}

func DataItems(value any) ([]any, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("source must resolve to an array")
	}
	if len(items) > MaxDataItems {
		return nil, fmt.Errorf("source exceeds %d items", MaxDataItems)
	}
	if _, err := DataResultBound(items); err != nil {
		return nil, err
	}
	return items, nil
}

func RunData(d *model.DataOperation, context map[string]any) (any, error) {
	source, err := Resolve(d.Source, context)
	if err != nil {
		return nil, err
	}
	if _, err = DataResultBound(source); err != nil {
		return nil, err
	}
	if d.Operation == "map" {
		return mapFields(d, source, context)
	}
	items, err := DataItems(source)
	if err != nil {
		return nil, err
	}
	out := []any{}
	switch d.Operation {
	case "filter":
		for i, item := range items {
			local := itemContext(context, item, i)
			keep, err := Evaluate(d.Predicate, local)
			if err != nil {
				return nil, err
			}
			if keep {
				out = append(out, item)
			}
		}
	case "limit":
		limit := d.Limit
		if limit > len(items) {
			limit = len(items)
		}
		out = append(out, items[:limit]...)
	case "count":
		return map[string]any{"count": len(items)}, nil
	case "sort":
		keys := make([]any, len(items))
		order := make([]int, len(items))
		kind := ""
		for i, item := range items {
			keys[i], err = dataKey(item, d.Key)
			if err != nil {
				return nil, err
			}
			current := "number"
			if _, ok := keys[i].(string); ok {
				current = "string"
			} else if _, ok := number(keys[i]); !ok {
				return nil, fmt.Errorf("sort field must be a string or number")
			}
			if kind != "" && kind != current {
				return nil, fmt.Errorf("sort fields must have the same type")
			}
			kind, order[i] = current, i
		}
		sort.SliceStable(order, func(i, j int) bool {
			a, b := keys[order[i]], keys[order[j]]
			if kind == "string" {
				if d.Descending {
					return a.(string) > b.(string)
				}
				return a.(string) < b.(string)
			}
			x, _ := number(a)
			y, _ := number(b)
			if d.Descending {
				return x > y
			}
			return x < y
		})
		for _, i := range order {
			out = append(out, items[i])
		}
	case "unique":
		seen := map[string]bool{}
		for _, item := range items {
			key, err := dataKey(item, d.Key)
			if err != nil {
				return nil, err
			}
			encoded, err := marshalData(key)
			if err != nil {
				return nil, fmt.Errorf("unique field must be JSON")
			}
			if !seen[string(encoded)] {
				seen[string(encoded)] = true
				out = append(out, item)
			}
		}
	case "sum":
		total := float64(0)
		for _, item := range items {
			key, err := dataKey(item, d.Key)
			if err != nil {
				return nil, err
			}
			n, ok := number(key)
			if !ok {
				return nil, fmt.Errorf("sum field must be numeric")
			}
			total += n
		}
		return DataResultBound(map[string]any{"sum": total, "count": len(items)})
	case "split":
		for _, item := range items {
			value, err := dataKey(item, d.Key)
			if err != nil {
				return nil, err
			}
			children, err := DataItems(value)
			if err != nil {
				return nil, err
			}
			if len(out)+len(children) > MaxDataItems {
				return nil, fmt.Errorf("split exceeds item limit")
			}
			out = append(out, children...)
		}
	case "append", "join", "compare":
		raw, err := Resolve(d.Other, context)
		if err != nil {
			return nil, err
		}
		other, err := DataItems(raw)
		if err != nil {
			return nil, err
		}
		if d.Operation == "append" {
			out = append(append(out, items...), other...)
			break
		}
		// Indexed matching avoids quadratic comparisons. JSON encodings preserve
		// type distinctions; missing fields fail rather than matching null.
		keyPath := d.OtherKey
		if d.Operation == "compare" {
			keyPath = d.Key
		}
		index := map[string][]any{}
		for _, item := range other {
			key, err := dataKey(item, keyPath)
			if err != nil {
				return nil, err
			}
			raw, err := marshalData(key)
			if err != nil {
				return nil, err
			}
			index[string(raw)] = append(index[string(raw)], item)
		}
		for _, item := range items {
			key, err := dataKey(item, d.Key)
			if err != nil {
				return nil, err
			}
			raw, err := marshalData(key)
			if err != nil {
				return nil, err
			}
			matches := index[string(raw)]
			if d.Operation == "compare" {
				if len(matches) == 0 {
					out = append(out, item)
				}
			} else {
				for _, right := range matches {
					if len(out) >= MaxDataItems {
						return nil, fmt.Errorf("join exceeds item limit")
					}
					out = append(out, map[string]any{"left": item, "right": right})
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported data operation")
	}
	if len(out) > MaxDataItems {
		return nil, fmt.Errorf("result exceeds item limit")
	}
	return DataResultBound(out)
}

func dataKey(item any, path string) (any, error) {
	if path == "" {
		return item, nil
	}
	return Lookup("item."+path, map[string]any{"item": item})
}

func itemContext(context map[string]any, item any, index int) map[string]any {
	local := map[string]any{}
	//workflowcheck:ignore copying data does not issue commands
	for k, v := range context {
		local[k] = v
	}
	local["item"], local["index"] = item, index
	return local
}

func mapFields(d *model.DataOperation, source any, context map[string]any) (any, error) {
	out := map[string]any{}
	if d.KeepSource {
		object, ok := source.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("keep_source requires an object")
		}
		//workflowcheck:ignore copying data does not issue commands
		for k, v := range object {
			out[k] = v
		}
	}
	local := itemContext(context, source, 0)
	for _, field := range d.Fields {
		value, err := Resolve(field.Value, local)
		if field.UseDefault && (err != nil || value == nil) {
			value, err = Resolve(field.Default, local)
		}
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		value, err = convertField(value, field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		out[field.Name] = value
	}
	return DataResultBound(out)
}

func convertField(value any, kind string) (any, error) {
	switch kind {
	case "":
		return value, nil
	case "string":
		if s, ok := value.(string); ok {
			return s, nil
		}
		raw, err := marshalData(value)
		return string(raw), err
	case "number", "integer":
		n, ok := number(value)
		if !ok {
			if s, isText := value.(string); isText {
				parsed, err := strconv.ParseFloat(s, 64)
				n, ok = parsed, err == nil
			}
		}
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || (kind == "integer" && (math.Trunc(n) != n || math.Abs(n) > 9007199254740991)) {
			return nil, fmt.Errorf("cannot convert to %s", kind)
		}
		return n, nil
	case "boolean":
		if b, ok := value.(bool); ok {
			return b, nil
		}
		if value == "true" {
			return true, nil
		}
		if value == "false" {
			return false, nil
		}
	case "object":
		if _, ok := value.(map[string]any); ok {
			return value, nil
		}
	case "array":
		if _, ok := value.([]any); ok {
			return value, nil
		}
	}
	return nil, fmt.Errorf("cannot convert to %s", kind)
}

func marshalData(value any) ([]byte, error) {
	if err := plainData(value, 0); err != nil {
		return nil, err
	}
	//workflowcheck:ignore plainData rejects custom marshalers and non-JSON types; encoding/json v1 sorts string map keys and is deterministic for these values
	return json.Marshal(value)
}

func plainData(value any, depth int) error {
	if depth > 32 {
		return fmt.Errorf("data nesting exceeds 32 levels")
	}
	switch v := value.(type) {
	case nil, bool, string, int, int32, int64:
		return nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("data contains non-finite numbers")
		}
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fmt.Errorf("data contains non-finite numbers")
		}
	case []any:
		for _, child := range v {
			if err := plainData(child, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		//workflowcheck:ignore keys sorted before recursion
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := plainData(v[key], depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("data must contain only JSON values")
	}
	return nil
}

// CanonicalDataJSON accepts only bounded, plain JSON-shaped values and sorts
// object keys. It is suitable for deterministic signal idempotency fingerprints.
func CanonicalDataJSON(value any) ([]byte, error) { return marshalData(value) }

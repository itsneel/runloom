package definition

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/dbnode"
)

// SQL text, identifiers and connection IDs are immutable, never interpolated.
func ResolveDatabase(op dbnode.Operation, values map[string]any) (dbnode.Operation, error) {
	payload := map[string]any{"parameters": op.Parameters, "values": op.Values, "filter": op.Filter, "pipeline": op.Pipeline}
	// Convert typed containers to JSON-shaped values for the shared resolver.
	b, err := json.Marshal(payload)
	if err != nil {
		return op, err
	}
	var plain any
	if err = json.Unmarshal(b, &plain); err != nil {
		return op, err
	}
	resolved, err := Resolve(plain, values)
	if err != nil {
		return op, err
	}
	b, err = json.Marshal(resolved)
	if err != nil {
		return op, err
	}
	var fields dbnode.Operation
	if err = json.Unmarshal(b, &fields); err != nil {
		return op, err
	}
	op.Parameters = fields.Parameters
	op.Values = fields.Values
	op.Filter = fields.Filter
	op.Pipeline = fields.Pipeline
	return op, nil
}

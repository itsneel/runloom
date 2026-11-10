package workflows_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func TestDataBuildingOperations(t *testing.T) {
	rows := []any{map[string]any{"id": "b", "amount": float64(3)}, map[string]any{"id": "a", "amount": float64(2)}, map[string]any{"id": "b", "amount": float64(3)}}
	ctx := map[string]any{"input": map[string]any{"rows": rows}}
	for _, tc := range []struct {
		name, kind string
		op         model.DataOperation
		want       any
	}{
		{"map", "transform", model.DataOperation{Operation: "map", Source: map[string]any{"id": "b"}, KeepSource: true, Fields: []model.FieldMapping{{Name: "total", Value: "7", Type: "integer"}, {Name: "fallback", Value: "${input.missing}", UseDefault: true, Default: false}}}, map[string]any{"id": "b", "total": float64(7), "fallback": false}},
		{"filter", "collection", model.DataOperation{Operation: "filter", Source: "${input.rows}", Predicate: &model.ConditionExpression{Left: "${item.amount}", Operator: "greater_than", Right: float64(2)}}, []any{rows[0], rows[2]}},
		{"sort", "collection", model.DataOperation{Operation: "sort", Source: rows, Key: "id"}, []any{rows[1], rows[0], rows[2]}},
		{"descending", "collection", model.DataOperation{Operation: "sort", Source: rows, Key: "amount", Descending: true}, []any{rows[0], rows[2], rows[1]}},
		{"unique", "collection", model.DataOperation{Operation: "unique", Source: rows, Key: "id"}, []any{rows[0], rows[1]}},
		{"limit", "collection", model.DataOperation{Operation: "limit", Source: rows, Limit: 1}, []any{rows[0]}},
		{"zero-limit", "collection", model.DataOperation{Operation: "limit", Source: rows}, []any{}},
		{"count", "collection", model.DataOperation{Operation: "count", Source: rows}, map[string]any{"count": 3}},
		{"sum", "collection", model.DataOperation{Operation: "sum", Source: rows, Key: "amount"}, map[string]any{"sum": float64(8), "count": 3}},
		{"append", "merge", model.DataOperation{Operation: "append", Source: []any{1}, Other: []any{2}}, []any{1, 2}},
		{"compare", "collection", model.DataOperation{Operation: "compare", Source: rows, Other: []any{rows[0]}, Key: "id"}, []any{rows[1]}},
		{"split", "collection", model.DataOperation{Operation: "split", Source: []any{map[string]any{"children": []any{1, 2}}}, Key: "children"}, []any{1, 2}},
		{"join", "merge", model.DataOperation{Operation: "join", Source: rows, Other: []any{map[string]any{"customer": "a"}}, Key: "id", OtherKey: "customer"}, []any{map[string]any{"left": rows[1], "right": map[string]any{"customer": "a"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, definition.ValidateData(&tc.op, tc.kind))
			got, err := definition.RunData(&tc.op, ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDataBuildingFailsClosed(t *testing.T) {
	for _, op := range []model.DataOperation{
		{Operation: "sort", Source: []any{map[string]any{"v": 1}, map[string]any{"v": "2"}}, Key: "v"},
		{Operation: "sum", Source: []any{map[string]any{"v": "2"}}, Key: "v"},
		{Operation: "unique", Source: []any{map[string]any{}}, Key: "missing"},
		{Operation: "count", Source: make([]any, 1001)},
		{Operation: "map", Fields: []model.FieldMapping{{Name: "n", Value: "NaN", Type: "number"}}},
		{Operation: "map", Fields: []model.FieldMapping{{Name: "n", Value: "1.5", Type: "integer"}}},
		{Operation: "map", Fields: []model.FieldMapping{{Name: "n", Value: "yes", Type: "boolean"}}},
		{Operation: "map", Fields: []model.FieldMapping{{Name: "n", Value: strings.Repeat("x", 262144)}}},
	} {
		_, err := definition.RunData(&op, map[string]any{})
		require.Error(t, err)
	}
	for _, kind := range []string{"transform", "collection", "merge"} {
		require.Error(t, definition.ValidateData(&model.DataOperation{Operation: "execute"}, kind))
	}
	require.Error(t, definition.ValidateData(&model.DataOperation{Operation: "map", Fields: []model.FieldMapping{{Name: "a"}, {Name: "a"}}}, "transform"))
}

func TestBuildingDraftRoundTripAndValidation(t *testing.T) {
	d := model.Definition{ID: "data-test", Name: "Data", SchemaVersion: "1.0", Steps: []model.Step{{ID: "map", Type: "transform", Data: &model.DataOperation{Operation: "map"}}}}
	normalized, err := definition.Normalize(d)
	require.NoError(t, err)
	raw, err := json.Marshal(normalized)
	require.NoError(t, err)
	var restored model.Definition
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.Equal(t, normalized, restored)
	d.Steps[0].HTTP = &model.HTTPRequest{URL: "https://example.com", Method: "GET"}
	_, err = definition.Normalize(d)
	require.Error(t, err)
	_, err = definition.Draft(d)
	require.NoError(t, err)
	require.NotEmpty(t, definition.NodeIssues(d, nil))
}

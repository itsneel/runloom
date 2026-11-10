package definition

import (
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func testContext() map[string]any {
	return map[string]any{
		"input": map[string]any{"name": "Avery", "count": 4.0},
		"steps": map[string]any{"approval": map[string]any{"output": map[string]any{"approved": true}}},
	}
}

func TestResolvePreservesCompleteReferenceType(t *testing.T) {
	value, err := Resolve("${input.count}", testContext())
	require.NoError(t, err)
	require.Equal(t, 4.0, value)
}

func TestResolveInterpolatesText(t *testing.T) {
	value, err := Resolve("Hello ${input.name}", testContext())
	require.NoError(t, err)
	require.Equal(t, "Hello Avery", value)
}

func TestResolveMissingReferenceFails(t *testing.T) {
	_, err := Resolve("${input.missing}", testContext())
	require.ErrorContains(t, err, "not found")
}

func TestEvaluateNumericAndExists(t *testing.T) {
	matched, err := Evaluate(&model.ConditionExpression{Left: "${input.count}", Operator: "greater_than", Right: 3.0}, testContext())
	require.NoError(t, err)
	require.True(t, matched)

	matched, err = Evaluate(&model.ConditionExpression{Left: "${input.missing}", Operator: "exists"}, testContext())
	require.NoError(t, err)
	require.False(t, matched)
}

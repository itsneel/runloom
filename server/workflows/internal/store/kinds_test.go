package store

import (
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDatabaseSummariesUseLatestPublishedKind(t *testing.T) {
	values := summarizeDefinitions([]definitionRecord{
		{ID: "flow", Version: 2, Body: model.Definition{ID: "flow", Version: 2, Kind: "child"}},
		{ID: "flow", Version: 0, Body: model.Definition{ID: "flow", Kind: "main"}},
		{ID: "flow", Version: 1, Body: model.Definition{ID: "flow", Version: 1, Kind: "main"}},
		{ID: "legacy", Version: 1, Body: model.Definition{ID: "legacy", Version: 1}},
		{ID: "draft", Version: 0, Body: model.Definition{ID: "draft", Kind: "child"}},
	})
	require.Len(t, values, 3)
	require.Empty(t, values[0].PublishedKind)
	require.Equal(t, "child", values[1].PublishedKind)
	require.Equal(t, 2, *values[1].LatestVersion)
	require.Equal(t, "main", values[2].PublishedKind)
}

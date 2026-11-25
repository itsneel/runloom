package rexeralien

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestPublishedLienDefinition(t *testing.T) {
	data, err := os.ReadFile("../../../../examples/rexera-lien-tag.json")
	require.NoError(t, err)
	var d model.Definition
	require.NoError(t, json.Unmarshal(data, &d))
	_, err = definition.Normalize(d)
	require.NoError(t, err)
	names := map[string]bool{"rexera.lien.read": true, "rexera.lien.classify": true, "rexera.lien.save": true, "rexera.lien.plan-qa": true, "rexera.lien.submit-qa": true, "rexera.lien.finish": true}
	var visit func([]model.Step)
	visit = func(steps []model.Step) {
		for _, step := range steps {
			if step.Type == "activity" {
				require.True(t, names[step.Activity], step.Activity)
			}
			for _, branch := range step.Branches {
				visit(branch.Steps)
			}
		}
	}
	visit(d.Steps)
}

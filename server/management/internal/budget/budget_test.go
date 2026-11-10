package budget

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMonthlyPolicy(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 15, 0, 0, time.FixedZone("IST", 19800))
	start, end := Month(now)
	require.Equal(t, "2026-10-01T00:00:00Z", start.Format(time.RFC3339))
	require.Equal(t, "2026-11-01T00:00:00Z", end.Format(time.RFC3339))
	p := Policy{Name: "Monthly", Enabled: true, SoftUSD: "0.1", HardUSD: "0.3", Reason: "test"}
	require.NoError(t, Validate(p))
	require.True(t, p.Applies("new-project", "new-workflow"))
	require.Len(t, Evaluate(p, "p", "Project", now, "0.299999999999", false), 1)
	require.Len(t, Evaluate(p, "p", "Project", now, "0.300000000000", true), 2)
	p.ExcludedWorkflows = []string{"skip"}
	require.False(t, p.Applies("p", "skip"))
	p.Projects = []string{"p"}
	require.False(t, p.Applies("other", "run"))
	p.Enabled = false
	require.False(t, p.Applies("p", "run"))
	for _, v := range []string{"-1", "NaN", "1e2", "1/2", "1000000000", "0.0000000000001"} {
		p.HardUSD = v
		require.Error(t, Validate(p))
	}
	p.HardUSD = "0.01"
	require.Error(t, Validate(p))
	p.SoftUSD = ""
	p.HardUSD = "0"
	require.NoError(t, Validate(p))
}

package decision

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDecisionContract(t *testing.T) {
	for _, tc := range []struct {
		body, mode string
		remaining  int
		valid      bool
	}{
		{`{"outcome":"actions","actions":["a"],"reason":"Legacy missing flag"}`, "single", 1, false},
		{`{"hitl_required":null,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`, "single", 1, false},
		{`{"hitl_required":"false","hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`, "single", 1, false},
		{`{"hitl_required":true,"hitl_reason":"Review","outcome":"actions","actions":["a"],"reason":"Try"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"handoff","actions":[],"reason":"Review"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Relevant"}`, "single", 1, true},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["b","a"],"reason":"Both"}`, "multiple", 2, true},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`, "agent", 0, true},
		{`{"hitl_required":true,"hitl_reason":"Human review requested","outcome":"handoff","actions":[],"reason":"Review"}`, "single", 1, true},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a","b"],"reason":"Both"}`, "agent", 2, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a","a"],"reason":"Both"}`, "multiple", 2, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a","b"],"reason":"Both"}`, "multiple", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["unknown"],"reason":"Try"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":[],"reason":"Empty"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":["a"],"reason":"Bad"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":null,"reason":"Bad"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done","code":"bad"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done","reason":"Again"}`, "single", 1, false},
		{`{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"} {}`, "single", 1, false},
		{"```json\n{}\n```", "single", 1, false},
	} {
		_, err := Parse(tc.body, []string{"a", "b"}, tc.mode, tc.remaining)
		if tc.valid {
			require.NoError(t, err, tc.body)
		} else {
			require.Error(t, err, tc.body)
		}
	}
}

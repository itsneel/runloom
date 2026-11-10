package llmcontract

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReviewEnvelope(t *testing.T) {
	for _, body := range []string{
		`{"content":"Answer","hitl_required":false,"hitl_reason":""}`,
		`{"content":"Draft","hitl_required":true,"hitl_reason":"Confirm the recipient"}`,
	} {
		text, _, err := Parse(body)
		require.NoError(t, err)
		require.NotEmpty(t, text)
	}
	for _, body := range []string{
		`{"content":"Answer","hitl_reason":""}`,
		`{"content":"Answer","hitl_required":null,"hitl_reason":""}`,
		`{"content":"Answer","hitl_required":"false","hitl_reason":""}`,
		`{"content":"Answer","hitl_required":0,"hitl_reason":""}`,
		`{"content":"Answer","hitl_required":false}`,
		`{"content":"Answer","hitl_required":false,"hitl_reason":null}`,
		`{"content":"Answer","hitl_required":true,"hitl_reason":" "}`,
		`{"content":"Answer","hitl_required":false,"hitl_reason":"Review"}`,
		`{"content":null,"hitl_required":false,"hitl_reason":""}`,
		`{"content":"Answer","hitl_required":true,"hitl_required":false,"hitl_reason":""}`,
		`{"content":"Answer","hitl_required":false,"hitl_reason":"","extra":true}`,
		`{"content":"Answer","hitl_required":false,"hitl_reason":""} {}`,
		`{"content":"Answer","hitl_required":false,"hitl_reason":""`,
		`plain text`,
	} {
		_, _, err := Parse(body)
		require.Error(t, err, body)
	}
}

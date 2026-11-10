package codeexec

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func TestOutputWriterCannotBypassLimit(t *testing.T) {
	var output limitedBuffer
	_, err := io.Copy(&output, bytes.NewReader(make([]byte, codecontract.MaxJSONBytes+8193)))
	require.Error(t, err)
	require.True(t, output.overflow)
	require.LessOrEqual(t, len(output.Bytes()), codecontract.MaxJSONBytes+8192)
}

func TestIsolatedProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runloom-code")
	build := exec.Command("go", "build", "-o", path, "../../cmd/code")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))
	t.Setenv("RUNLOOM_CODE_RUNNER", path)
	t.Setenv("RUNLOOM_TEST_SECRET", "not-for-code")
	input := model.CodeActivityInput{TeamID: "team-a", Code: model.JavaScriptCode{Runtime: codecontract.Runtime, Source: "return {answer: input.value * 2}"}, Input: map[string]any{"value": 21}}
	value, err := Execute(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"answer": float64(42)}, value)
	input.Code.Source = "while (true) {}"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = Execute(ctx, input)
	require.ErrorContains(t, err, "canceled")
	input.Code.Source = "return null"
	_, err = Execute(context.Background(), input)
	require.NoError(t, err, "cancellation must release capacity")
	t.Setenv("RUNLOOM_CODE_RUNNER", filepath.Join(t.TempDir(), "missing"))
	_, err = Execute(context.Background(), input)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestCapacityBoundaries(t *testing.T) {
	input := model.CodeActivityInput{TeamID: "team-a", Code: model.JavaScriptCode{Runtime: codecontract.Runtime, Source: "return true"}}
	capacity.Lock()
	capacity.total = 1
	capacity.teams["team-a"] = true
	capacity.Unlock()
	t.Cleanup(func() { capacity.Lock(); capacity.total = 0; capacity.teams = map[string]bool{}; capacity.Unlock() })
	_, err := Execute(context.Background(), input)
	require.ErrorIs(t, err, ErrBusy)
	capacity.Lock()
	capacity.total = 2
	capacity.Unlock()
	input.TeamID = "team-b"
	_, err = Execute(context.Background(), input)
	require.ErrorIs(t, err, ErrBusy)
}

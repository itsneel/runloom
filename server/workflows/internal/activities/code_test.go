package activities

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/codeexec"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func TestCodeErrorRetryContract(t *testing.T) {
	_, err := Code(context.Background(), model.CodeActivityInput{})
	var applicationError *temporal.ApplicationError
	require.True(t, errors.As(err, &applicationError))
	require.True(t, applicationError.NonRetryable())
	require.Equal(t, "JavaScriptError", applicationError.Type())
	t.Setenv("RUNLOOM_CODE_RUNNER", filepath.Join(t.TempDir(), "missing"))
	_, err = Code(context.Background(), model.CodeActivityInput{TeamID: "team", Code: model.JavaScriptCode{Runtime: codecontract.Runtime, Source: "return true"}})
	require.ErrorIs(t, err, codeexec.ErrUnavailable)
	require.False(t, errors.As(err, &applicationError), "infrastructure failures may retry")
}

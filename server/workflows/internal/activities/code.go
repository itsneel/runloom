package activities

import (
	"context"
	"errors"

	"github.com/example/runloom/workflows/internal/codeexec"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
)

func Code(ctx context.Context, input model.CodeActivityInput) (any, error) {
	output, err := codeexec.Execute(ctx, input)
	if err == nil {
		return output, nil
	}
	if errors.Is(err, codeexec.ErrBusy) || errors.Is(err, codeexec.ErrUnavailable) {
		return nil, err
	}
	return nil, temporal.NewNonRetryableApplicationError(err.Error(), "JavaScriptError", nil)
}

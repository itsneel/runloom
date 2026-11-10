package activities

import (
	"context"
	"github.com/example/runloom/workflows/internal/mailnode"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
)

type Email struct{ Sender *mailnode.Sender }

func (a *Email) Send(ctx context.Context, input model.EmailActivityInput) (mailnode.Result, error) {
	result, err := a.Sender.Send(ctx, input.Message)
	if err != nil {
		return result, temporal.NewNonRetryableApplicationError(err.Error(), "EmailError", nil)
	}
	return result, nil
}

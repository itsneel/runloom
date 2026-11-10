package activities

import (
	"context"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
)

func (a *HTTP) Integration(ctx context.Context, input model.IntegrationInput) (map[string]any, error) {
	request, err := definition.BuildIntegration(input.Integration)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidIntegration", nil)
	}
	output, err := a.Call(ctx, model.HTTPActivityInput{TeamID: input.TeamID, Request: request})
	if err != nil {
		return nil, err
	}
	if err := definition.ValidateIntegrationResponse(input.Integration.Operation, output); err != nil {
		return nil, temporal.NewNonRetryableApplicationError(err.Error(), "IntegrationRejected", nil)
	}
	return output, nil
}

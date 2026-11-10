package services

import (
	"context"
	"github.com/example/runloom/auth/internal/model"
)

type Backend struct {
	Team      string
	Workflows *Client
}

func (b *Backend) Get(id string, v *int) (out model.Definition, err error) {
	err = b.Workflows.call(context.Background(), "teams/"+b.Team+"/definition/get", struct {
		ID      string
		Version *int
	}{id, v}, &out)
	return
}
func (b *Backend) Close(context.Context) error { return nil }

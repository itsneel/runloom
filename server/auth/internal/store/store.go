package store

import (
	"context"
	"errors"
	"github.com/example/runloom/auth/internal/model"
)

var ErrNotFound = errors.New("workflow not found")

type Backend interface {
	Get(string, *int) (model.Definition, error)
	Close(context.Context) error
}

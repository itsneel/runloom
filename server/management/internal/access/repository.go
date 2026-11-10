package access

import (
	"context"
)

type Repository interface {
	Module(string) Repository
	ReadOwned(context.Context) (State, int64, error)
	Read(context.Context) (State, int64, error)
	Swap(context.Context, int64, State) (bool, error)
	Close(context.Context) error
}

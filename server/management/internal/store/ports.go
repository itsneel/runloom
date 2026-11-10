package store

import (
	"context"

	"github.com/example/runloom/management/internal/budget"

	"github.com/example/runloom/management/internal/prompts"
	"github.com/example/runloom/management/internal/usage"
)

type Prompts interface {
	ListPrompts(context.Context) ([]prompts.Entry, error)
	GetPrompt(context.Context, string, int) (prompts.Entry, error)
	SavePrompt(context.Context, prompts.Entry, bool, string) (prompts.Entry, error)
	PromptHistory(context.Context, string) ([]prompts.Entry, error)
}

type Metering interface {
	BeginUsage(context.Context, usage.Entry) (bool, error)
	FinishUsage(context.Context, usage.Entry) error
	QueryUsage(context.Context, usage.Filter) ([]usage.Entry, error)
	AggregateSpend(context.Context, string, string) ([]SpendTotal, error)
}

type Policies interface {
	SaveBudgetPolicy(context.Context, budget.Policy, string) (budget.Policy, error)
	ProjectCatalog(context.Context) (ProjectCatalog, error)
}

type Lifecycle interface {
	Health(context.Context) bool
	Close(context.Context) error
	Driver() string
}

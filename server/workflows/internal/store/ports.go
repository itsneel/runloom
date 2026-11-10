package store

import (
	"context"
	"time"

	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/usage"
)

// Catalog is the authoring boundary. Execution consumers receive published
// snapshots through this port, never a database connection.
type Catalog interface {
	DeleteDraft(context.Context, string) error
	SaveDraft(model.Definition) (model.Definition, error)
	CommitDraft(model.Definition, CommitMeta) (model.Definition, DefinitionCommit, error)
	Publish(string) (model.Definition, error)
	PublishWithCommit(string, CommitMeta) (model.Definition, error)
	Get(string, *int) (model.Definition, error)
	List() ([]Summary, error)
	ListCommits(string, int) ([]DefinitionCommit, error)
	GetCommit(string, string) (DefinitionCommit, error)
	RestoreCommit(string, string, CommitMeta) (model.Definition, DefinitionCommit, error)
}

type Prompts interface {
	ListPrompts(context.Context) ([]prompts.Entry, error)
	GetPrompt(context.Context, string, int) (prompts.Entry, error)
	SavePrompt(context.Context, prompts.Entry, bool, string) (prompts.Entry, error)
	PromptHistory(context.Context, string) ([]prompts.Entry, error)
}

type Projects interface {
	DeleteProject(context.Context, string) error
	ProjectCatalog(context.Context) (ProjectCatalog, error)
	CreateProject(context.Context, string) (Project, error)
	RenameProject(context.Context, string, string) (Project, error)
	AssignProject(context.Context, string, string) error
	EditLock(context.Context, string) (EditLockStatus, error)
	ChangeEditLock(context.Context, string, string, string, string, string) (EditLockStatus, error)
	BeginDefinitionWrite(context.Context, string, string, string) (func() error, error)
}

type Runs interface {
	RecordRunStart(RunRecord) (RunRecord, error)
	UpdateRun(string, string, *model.RunState) (bool, error)
	ObserveRun(context.Context, string, string, *model.RunState, time.Time) (bool, error)
	GetRun(string) (RunRecord, error)
	ListRuns(int) ([]RunRecord, error)
	SearchRuns(context.Context, RunFilter) ([]RunRecord, error)
	PendingRuns(context.Context, string, int) ([]RunRecord, error)
}

type Audits interface {
	AppendAudit(AuditEvent) (AuditEvent, error)
	ListAudit(string, string, int) ([]AuditEvent, error)
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

// Modules can be backed by different local adapters or service clients. Only
// composition roots need Backend; handlers depend on the individual ports.
type Modules struct {
	Prompts   Prompts
	Catalog   Catalog
	Projects  Projects
	Runs      Runs
	Audits    Audits
	Metering  Metering
	Policies  Policies
	Lifecycle Lifecycle
}

func Bind(b Backend) Modules {
	return Modules{Prompts: b, Catalog: b, Projects: b, Runs: b, Audits: b, Metering: b, Policies: b, Lifecycle: b}
}

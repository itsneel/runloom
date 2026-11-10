package model

import (
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/example/runloom/workflows/internal/llmcontract"
	"github.com/example/runloom/workflows/internal/prompts"
)

type Definition struct {
	SignalHandlers []SignalHandler `json:"signal_handlers,omitempty"`
	Layout         *CanvasLayout   `json:"layout,omitempty"`
	Kind           string          `json:"kind,omitempty"`
	SchemaVersion  string          `json:"schema_version"`
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Version        int             `json:"version"`
	Trigger        Trigger         `json:"trigger"`
	InputSchema    map[string]any  `json:"input_schema"`
	Steps          []Step          `json:"steps"`
	Output         any             `json:"output"`
}

// Canvas coordinates are authoring metadata only; the interpreter ignores them.
type CanvasLayout struct {
	Positions map[string]CanvasPoint `json:"positions"`
}
type CanvasPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Trigger struct {
	Type   string `json:"type"`
	Path   string `json:"path,omitempty"`
	Method string `json:"method,omitempty"`
}

type Step struct {
	Integration    *Integration         `json:"integration,omitempty"`
	LLMTask        *llmcontract.Task    `json:"llm_task,omitempty"`
	Data           *DataOperation       `json:"data,omitempty"`
	ForEach        *ForEach             `json:"foreach,omitempty"`
	Database       *dbnode.Operation    `json:"database,omitempty"`
	PromptTemplate *prompts.Template    `json:"prompt_template,omitempty"`
	PromptRef      *prompts.Reference   `json:"prompt_ref,omitempty"`
	PromptInputs   map[string]any       `json:"prompt_inputs,omitempty"`
	OutputSchema   map[string]any       `json:"output_schema,omitempty"`
	Decision       *DecisionConfig      `json:"decision,omitempty"`
	Email          *EmailMessage        `json:"email,omitempty"`
	Code           *JavaScriptCode      `json:"code,omitempty"`
	HTTP           *HTTPRequest         `json:"http,omitempty"`
	DefinitionID   string               `json:"definition_id,omitempty"`
	Version        int                  `json:"version,omitempty"`
	ID             string               `json:"id"`
	Name           string               `json:"name,omitempty"`
	Type           string               `json:"type"`
	Activity       string               `json:"activity,omitempty"`
	With           map[string]any       `json:"with,omitempty"`
	Model          string               `json:"model,omitempty"`
	SystemPrompt   string               `json:"system_prompt,omitempty"`
	Temperature    *float64             `json:"temperature,omitempty"`
	MaxTokens      *int                 `json:"max_tokens,omitempty"`
	TimeoutSeconds *int                 `json:"timeout_seconds,omitempty"`
	Retry          *RetryPolicy         `json:"retry,omitempty"`
	Seconds        int                  `json:"seconds,omitempty"`
	Prompt         string               `json:"prompt,omitempty"`
	CallbackKey    string               `json:"callback_key,omitempty"`
	Message        string               `json:"message,omitempty"`
	SignalName     string               `json:"signal_name,omitempty"`
	OnTimeout      string               `json:"on_timeout,omitempty"`
	If             *ConditionExpression `json:"if,omitempty"`
	Then           []Step               `json:"then,omitempty"`
	Else           []Step               `json:"else,omitempty"`
	Branches       []Branch             `json:"branches,omitempty"`
}

// SMTP connection, sender identity and credentials are worker configuration,
// never workflow fields. Bcc is omitted from message headers, not run history.
type EmailMessage struct {
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	BCC     []string `json:"bcc,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html,omitempty"`
}

type EmailActivityInput struct {
	TeamID  string       `json:"team_id"`
	Message EmailMessage `json:"message"`
}

// Runtime is pinned in published definitions; source is literal, never interpolated.
type JavaScriptCode struct {
	Runtime string `json:"runtime"`
	Source  string `json:"source"`
}

type CodeActivityInput struct {
	TeamID string         `json:"team_id"`
	Code   JavaScriptCode `json:"code"`
	Input  map[string]any `json:"input"`
}

// Secret values are resolved only inside the worker activity, never in workflow history.
type SecretHeader struct {
	Secret string `json:"secret"`
	Prefix string `json:"prefix,omitempty"`
}
type HTTPRequest struct {
	URL           string                  `json:"url"`
	Method        string                  `json:"method"`
	Headers       map[string]string       `json:"headers,omitempty"`
	SecretHeaders map[string]SecretHeader `json:"secret_headers,omitempty"`
	Body          any                     `json:"body,omitempty"`
}
type HTTPActivityInput struct {
	TeamID  string      `json:"team_id"`
	Request HTTPRequest `json:"request"`
}

type RetryPolicy struct {
	MaximumAttempts    int     `json:"maximum_attempts"`
	InitialInterval    float64 `json:"initial_interval_seconds"`
	BackoffCoefficient float64 `json:"backoff_coefficient"`
	MaximumInterval    float64 `json:"maximum_interval_seconds"`
}

type ConditionExpression struct {
	Left     any    `json:"left"`
	Operator string `json:"operator"`
	Right    any    `json:"right,omitempty"`
}

type Branch struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Steps       []Step `json:"steps"`
}

type DecisionConfig struct {
	Mode       string `json:"mode"`
	Execution  string `json:"execution"`
	MaxRounds  int    `json:"max_rounds"`
	MaxActions int    `json:"max_actions"`
}

type RunInput struct {
	Metadata         *ExecutionMetadata           `json:"metadata,omitempty"`
	ChildMetadata    map[string]ExecutionMetadata `json:"child_metadata,omitempty"`
	IsTest           bool                         `json:"is_test,omitempty"`
	Definitions      map[string]Definition        `json:"definitions,omitempty"`
	TeamID           string                       `json:"team_id,omitempty"`
	Source           string                       `json:"source,omitempty"`
	Actor            string                       `json:"actor,omitempty"`
	ParentWorkflowID string                       `json:"parent_workflow_id,omitempty"`
	ScheduleID       string                       `json:"schedule_id,omitempty"`
	Definition       Definition                   `json:"definition"`
	Input            map[string]any               `json:"input"`
}

type RunResult struct {
	SignalHandlers map[string]SignalHandlerState `json:"signal_handlers,omitempty"`
	Status         string                        `json:"status"`
	Output         any                           `json:"output"`
	Steps          map[string]StepOutput         `json:"steps"`
}

type StepOutput struct {
	Output any `json:"output"`
}

type ApprovalDecision struct {
	StepID   string `json:"step_id"`
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
	Actor    string `json:"actor"`
}

type CurrentStep struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type ApprovalGate struct {
	StepID string `json:"step_id"`
	Prompt string `json:"prompt"`
}

type CallbackGate struct {
	StepID      string `json:"step_id"`
	CallbackKey string `json:"callback_key"`
	Prompt      string `json:"prompt"`
}

type PauseGate struct {
	Budget  *budget.Decision `json:"budget,omitempty"`
	StepID  string           `json:"step_id"`
	Message string           `json:"message"`
}

type SignalGate struct {
	StepID     string `json:"step_id"`
	SignalName string `json:"signal_name"`
	Prompt     string `json:"prompt"`
}

type WorkflowEvent struct {
	EventID    string `json:"event_id,omitempty"`
	StepID     string `json:"step_id,omitempty"`
	SignalName string `json:"signal_name,omitempty"`
	Payload    any    `json:"payload"`
	Actor      string `json:"actor"`
}

type StepState struct {
	Status string `json:"status"`
	Output any    `json:"output"`
}

type RunState struct {
	SignalHandlers    map[string]SignalHandlerState `json:"signal_handlers,omitempty"`
	Metadata          *ExecutionMetadata            `json:"metadata,omitempty"`
	IsTest            bool                          `json:"is_test,omitempty"`
	ChildWorkflows    map[string]string             `json:"child_workflows,omitempty"`
	ParentWorkflowID  string                        `json:"parent_workflow_id,omitempty"`
	ScheduleID        string                        `json:"schedule_id,omitempty"`
	Status            string                        `json:"status"`
	DefinitionID      string                        `json:"definition_id,omitempty"`
	DefinitionVersion int                           `json:"definition_version,omitempty"`
	CurrentStep       *CurrentStep                  `json:"current_step"`
	CompletedSteps    []string                      `json:"completed_steps"`
	WaitingApproval   *ApprovalGate                 `json:"waiting_approval"`
	WaitingApprovals  map[string]ApprovalGate       `json:"waiting_approvals"`
	WaitingCallback   *CallbackGate                 `json:"waiting_callback"`
	WaitingCallbacks  map[string]CallbackGate       `json:"waiting_callbacks"`
	PausedStep        *PauseGate                    `json:"paused_step"`
	PausedSteps       map[string]PauseGate          `json:"paused_steps"`
	WaitingSignal     *SignalGate                   `json:"waiting_signal"`
	WaitingSignals    map[string]SignalGate         `json:"waiting_signals"`
	Steps             map[string]StepState          `json:"steps"`
	Error             *string                       `json:"error"`
	StartedAt         string                        `json:"started_at,omitempty"`
	CompletedAt       string                        `json:"completed_at,omitempty"`
	Output            any                           `json:"output,omitempty"`
}

// Signal handlers are separate execution paths, never inline wait nodes.
type SignalHandler struct {
	ID          string         `json:"id"`
	Name        string         `json:"name,omitempty"`
	SignalName  string         `json:"signal_name"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	Steps       []Step         `json:"steps"`
}
type SignalHandlerState struct {
	Output       any            `json:"output,omitempty"`
	ID           string         `json:"id"`
	SignalName   string         `json:"signal_name"`
	Status       string         `json:"status"`
	Accepting    bool           `json:"accepting"`
	Received     int            `json:"received"`
	Completed    int            `json:"completed"`
	Pending      int            `json:"pending"`
	InvocationID string         `json:"invocation_id,omitempty"`
	Error        string         `json:"error,omitempty"`
	InputSchema  map[string]any `json:"input_schema,omitempty"`
}

// ExecutionSteps is read-only traversal of all entry paths. Use original path
// slices when mutating definitions, rather than assigning into this flat copy.
func ExecutionSteps(d Definition) []Step {
	steps := append([]Step(nil), d.Steps...)
	for _, handler := range d.SignalHandlers {
		steps = append(steps, handler.Steps...)
	}
	return steps
}

// Handler-enabled snapshots use new workflow types. Old workers cannot silently
// decode away the new field and execute only the main path during a rollout.
func WorkflowType(base string, input RunInput) string {
	if len(input.Definition.SignalHandlers) > 0 {
		return base + "WithSignalHandlers"
	}
	//workflowcheck:ignore pure feature predicate; iteration order cannot change the result
	for _, d := range input.Definitions {
		if len(d.SignalHandlers) > 0 {
			return base + "WithSignalHandlers"
		}
	}
	return base
}

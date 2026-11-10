package model

import "encoding/json"

// Node executions are a read-only projection assembled by the interpreter.
// Their sequence numbers are local to one Temporal run.
type ExecutionTrace struct {
	Nodes   []*NodeExecution `json:"nodes"`
	Omitted int              `json:"omitted,omitempty"`
}

type NodeExecution struct {
	Sequence      int             `json:"sequence"`
	Parent        int             `json:"parent,omitempty"`
	StepID        string          `json:"step_id"`
	Name          string          `json:"name,omitempty"`
	Type          string          `json:"type"`
	Scope         string          `json:"scope,omitempty"`
	Status        string          `json:"status"`
	StartedAt     string          `json:"started_at"`
	FinishedAt    string          `json:"finished_at,omitempty"`
	Input         json.RawMessage `json:"input,omitempty"`
	Output        json.RawMessage `json:"output,omitempty"`
	InputOmitted  bool            `json:"input_omitted,omitempty"`
	OutputOmitted bool            `json:"output_omitted,omitempty"`
	Error         string          `json:"error,omitempty"`
}

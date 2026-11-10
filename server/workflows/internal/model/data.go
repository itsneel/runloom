package model

// DataOperation is a bounded, declarative operation, not executable source.
// Values may reference input/steps; collection predicates also receive item.
type DataOperation struct {
	Operation  string               `json:"operation"`
	Source     any                  `json:"source,omitempty"`
	Other      any                  `json:"other,omitempty"`
	Fields     []FieldMapping       `json:"fields,omitempty"`
	KeepSource bool                 `json:"keep_source,omitempty"`
	Predicate  *ConditionExpression `json:"predicate,omitempty"`
	Key        string               `json:"key,omitempty"`
	OtherKey   string               `json:"other_key,omitempty"`
	Descending bool                 `json:"descending,omitempty"`
	Limit      int                  `json:"limit,omitempty"`
}

type FieldMapping struct {
	Name       string `json:"name"`
	Value      any    `json:"value"`
	Type       string `json:"type,omitempty"`
	UseDefault bool   `json:"use_default,omitempty"`
	Default    any    `json:"default,omitempty"`
}

// ForEach has one body branch. Output is evaluated separately for every item.
type ForEach struct {
	Items       any `json:"items"`
	Output      any `json:"output"`
	Concurrency int `json:"concurrency"`
}

type Integration struct {
	Operation  string         `json:"operation"`
	Secret     string         `json:"secret"`
	Parameters map[string]any `json:"parameters"`
}

type IntegrationInput struct {
	TeamID      string      `json:"team_id"`
	Integration Integration `json:"integration"`
}

// Package authoring owns bounded draft-authoring policy, outside Temporal replay.
package authoring

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
)

type Node struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	DefinitionID string `json:"definition_id,omitempty"`
	Version      int    `json:"version,omitempty"`
	Children     []Node `json:"children,omitempty"`
}
type Outline struct {
	ID         string            `json:"id"`
	Version    int               `json:"version"`
	Kind       string            `json:"kind"`
	Nodes      []Node            `json:"nodes"`
	Definition *model.Definition `json:"definition,omitempty"`
}
type Context struct {
	Integrations []definition.IntegrationSpec `json:"integrations,omitempty"`
	RootID       string                       `json:"root_id"`
	Revision     string                       `json:"revision"`
	ProjectID    string                       `json:"project_id"`
	Workflows    []Outline                    `json:"workflows"`
}
type Proposal struct {
	Name         string            `json:"name"`
	Steps        []model.Step      `json:"steps"`
	BeforeStepID string            `json:"before_step_id"`
	Operation    string            `json:"operation,omitempty"`
	Definition   *model.Definition `json:"definition,omitempty"`
}

func Fingerprint(value any) string {
	b, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// Authorized workflow definitions reach the configured provider. Credential stores
// and connection records are never loaded by this graph operation.
func Graph(root model.Definition, project string, get func(string, *int) (model.Definition, error)) (Context, error) {
	result := Context{RootID: root.ID, ProjectID: project, Workflows: []Outline{}}
	hashes := map[string]string{}
	visiting := map[string]bool{}
	depths := map[string]int{}
	var visit func(model.Definition, int) error
	visit = func(d model.Definition, depth int) error {
		if depth > definition.MaxSubworkflowDepth {
			return fmt.Errorf("child nesting exceeds %d levels", definition.MaxSubworkflowDepth)
		}
		if visiting[d.ID] {
			return fmt.Errorf("circular child reference")
		}
		key := definition.DefinitionKey(d.ID, d.Version)
		if previous, ok := depths[key]; ok && previous >= depth {
			return nil
		}
		_, exists := hashes[key]
		if !exists && len(hashes) >= definition.MaxSubworkflowDefinitions {
			return fmt.Errorf("too many child definitions")
		}
		hashes[key] = Fingerprint(d)
		visiting[d.ID] = true
		defer delete(visiting, d.ID)
		var nodes func([]model.Step) ([]Node, error)
		nodes = func(steps []model.Step) ([]Node, error) {
			out := []Node{}
			for _, s := range steps {
				n := Node{ID: s.ID, Type: s.Type, DefinitionID: s.DefinitionID, Version: s.Version}
				if s.Type == "subworkflow" {
					if s.DefinitionID == "" || s.Version < 0 {
						return nil, fmt.Errorf("child reference is incomplete")
					}
					child, err := get(s.DefinitionID, &s.Version)
					if err != nil || child.Kind != "child" {
						return nil, fmt.Errorf("referenced child is unavailable in this team")
					}
					if err = visit(child, depth+1); err != nil {
						return nil, err
					}
				}
				nested := append(append([]model.Step{}, s.Then...), s.Else...)
				for _, b := range s.Branches {
					nested = append(nested, b.Steps...)
				}
				var err error
				n.Children, err = nodes(nested)
				if err != nil {
					return nil, err
				}
				out = append(out, n)
			}
			return out, nil
		}
		ns, err := nodes(model.ExecutionSteps(d))
		if err != nil {
			return err
		}
		if !exists {
			result.Workflows = append(result.Workflows, Outline{ID: d.ID, Version: d.Version, Kind: d.Kind, Nodes: ns})
		}
		hashes[key] = Fingerprint(d)
		depths[key] = depth
		return nil
	}
	if err := visit(root, 0); err != nil {
		return result, err
	}
	sort.Slice(result.Workflows, func(i, j int) bool {
		return definition.DefinitionKey(result.Workflows[i].ID, result.Workflows[i].Version) < definition.DefinitionKey(result.Workflows[j].ID, result.Workflows[j].Version)
	})
	result.Revision = Fingerprint(struct {
		Project string
		Hashes  map[string]string
	}{project, hashes})
	raw, _ := json.Marshal(result)
	if len(raw) > 65536 {
		return result, fmt.Errorf("authoring context exceeds 64 KiB")
	}
	return result, nil
}

// Build changes only the root's top-level sequence. Existing nodes and metadata
// are preserved. New children are never published implicitly.
func Build(root model.Definition, scope Context, id string, p Proposal) (model.Definition, model.Definition, error) {
	if root.ID != scope.RootID || id == root.ID {
		return root, model.Definition{}, fmt.Errorf("invalid authoring scope")
	}
	if len(strings.TrimSpace(p.Name)) == 0 || len(p.Name) > 160 || len(p.Steps) == 0 {
		return root, model.Definition{}, fmt.Errorf("child needs a name and at least one node")
	}
	allowed := map[string]bool{}
	for _, d := range scope.Workflows {
		if d.Kind == "child" && d.ID != root.ID {
			allowed[definition.DefinitionKey(d.ID, d.Version)] = true
		}
	}
	var check func([]model.Step) error
	check = func(steps []model.Step) error {
		for _, s := range steps {
			if s.Type == "subworkflow" && (!allowed[definition.DefinitionKey(s.DefinitionID, s.Version)] || s.DefinitionID == id) {
				return fmt.Errorf("child reference is outside the authorized root graph")
			}
			if s.HTTP != nil {
				if err := definition.ValidateHTTP(s.HTTP); err != nil {
					return err
				}
			}
			if err := check(s.Then); err != nil {
				return err
			}
			if err := check(s.Else); err != nil {
				return err
			}
			for _, b := range s.Branches {
				if err := check(b.Steps); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(p.Steps); err != nil {
		return root, model.Definition{}, err
	}
	child, err := definition.Draft(model.Definition{SchemaVersion: "1.0", ID: id, Name: p.Name, Kind: "child", Trigger: model.Trigger{Type: "manual"}, Steps: p.Steps, InputSchema: map[string]any{}})
	if err != nil {
		return root, child, err
	}
	// Require runnable node configurations for generated children. Draft-only
	// references may still prevent publication until explicitly pinned.
	candidate := child
	var pinForValidation func([]model.Step)
	pinForValidation = func(steps []model.Step) {
		for i := range steps {
			if steps[i].Type == "subworkflow" && steps[i].Version == 0 {
				steps[i].Version = 1
			}
			pinForValidation(steps[i].Then)
			pinForValidation(steps[i].Else)
			for j := range steps[i].Branches {
				pinForValidation(steps[i].Branches[j].Steps)
			}
		}
	}
	raw, _ := json.Marshal(child)
	_ = json.Unmarshal(raw, &candidate)
	pinForValidation(candidate.Steps)
	for i := range candidate.SignalHandlers {
		pinForValidation(candidate.SignalHandlers[i].Steps)
	}
	if _, err = definition.Normalize(candidate); err != nil {
		return root, child, err
	}
	at := len(root.Steps)
	if p.BeforeStepID != "" {
		at = -1
		for i, s := range root.Steps {
			if s.ID == p.BeforeStepID {
				at = i
				break
			}
		}
		if at < 0 {
			return root, child, fmt.Errorf("insertion node no longer exists at the root level")
		}
	}
	if p.BeforeStepID == "" {
		for i, s := range root.Steps {
			if s.Type == "end" {
				at = i
				break
			}
		}
	}
	node := model.Step{ID: "call-" + strings.TrimPrefix(id, "child-"), Name: p.Name, Type: "subworkflow", DefinitionID: id, Version: 0, With: map[string]any{}}
	var collision func([]model.Step) bool
	collision = func(steps []model.Step) bool {
		for _, s := range steps {
			if s.ID == node.ID || collision(s.Then) || collision(s.Else) {
				return true
			}
			for _, b := range s.Branches {
				if collision(b.Steps) {
					return true
				}
			}
		}
		return false
	}
	if collision(model.ExecutionSteps(root)) {
		return root, child, fmt.Errorf("attachment node ID already exists")
	}
	root.Steps = append(root.Steps, model.Step{})
	copy(root.Steps[at+1:], root.Steps[at:])
	root.Steps[at] = node
	root, err = definition.Draft(root)
	return root, child, err
}

// BuildWorkflow replaces only the selected root draft, or creates a new main draft.
// Identity, kind and project ownership of existing workflows remain server-controlled.
func BuildWorkflow(root model.Definition, scope Context, id string, p Proposal) (model.Definition, error) {
	if p.Definition == nil {
		return root, fmt.Errorf("a complete workflow definition is required")
	}
	d := *p.Definition
	if p.Operation == "create" {
		if scope.RootID != "" {
			return root, fmt.Errorf("create a standalone workflow from general chat")
		}
		d.ID = id
		d.Kind = "main"
	} else if p.Operation == "update" {
		if root.ID == "" || scope.RootID != root.ID {
			return root, fmt.Errorf("open the workflow to edit it")
		}
		if d.ID != root.ID || d.Kind != root.Kind {
			return root, fmt.Errorf("workflow ID and kind must remain unchanged")
		}
	} else {
		return root, fmt.Errorf("unsupported workflow operation")
	}
	d.SchemaVersion = "1.0"
	d.Version = 0
	// New references must already be in the authorized reachable graph.
	allowed := map[string]bool{}
	for _, ref := range scope.Workflows {
		if ref.ID != root.ID && ref.Kind == "child" {
			allowed[definition.DefinitionKey(ref.ID, ref.Version)] = true
		}
	}
	var check func([]model.Step) error
	check = func(steps []model.Step) error {
		for _, s := range steps {
			if s.Type == "subworkflow" && !allowed[definition.DefinitionKey(s.DefinitionID, s.Version)] {
				return fmt.Errorf("child reference is outside the authorized graph")
			}
			if err := check(s.Then); err != nil {
				return err
			}
			if err := check(s.Else); err != nil {
				return err
			}
			for _, b := range s.Branches {
				if err := check(b.Steps); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(model.ExecutionSteps(d)); err != nil {
		return root, err
	}
	// Validate complete runnable node configuration, retaining draft child pins as zero.
	raw, _ := json.Marshal(d)
	var candidate model.Definition
	_ = json.Unmarshal(raw, &candidate)
	var pin func([]model.Step)
	pin = func(steps []model.Step) {
		for i := range steps {
			if steps[i].Type == "subworkflow" && steps[i].Version == 0 {
				steps[i].Version = 1
			}
			pin(steps[i].Then)
			pin(steps[i].Else)
			for j := range steps[i].Branches {
				pin(steps[i].Branches[j].Steps)
			}
		}
	}
	pin(candidate.Steps)
	for i := range candidate.SignalHandlers {
		pin(candidate.SignalHandlers[i].Steps)
	}
	if _, err := definition.Normalize(candidate); err != nil {
		return root, err
	}
	return definition.Draft(d)
}

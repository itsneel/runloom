package definition

import (
	"fmt"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
)

func ResolvePrompts(d model.Definition, get func(string, int) (prompts.Entry, error)) (model.Definition, error) {
	count := 0
	var walk func([]model.Step, int) error
	walk = func(steps []model.Step, depth int) error {
		if depth > MaxDepth {
			return fmt.Errorf("prompt resolution exceeds node nesting limit")
		}
		for i := range steps {
			count++
			if count > MaxSteps {
				return fmt.Errorf("too many nodes")
			}
			s := &steps[i]
			if s.PromptRef != nil {
				ref := s.PromptRef
				if !prompts.Name.MatchString(ref.ID) || ref.Version < 1 {
					return fmt.Errorf("prompt references require an ID and published version")
				}
				e, err := get(ref.ID, ref.Version)
				if err != nil {
					return fmt.Errorf("prompt %s v%d is unavailable in this team: %w", ref.ID, ref.Version, err)
				}
				s.PromptTemplate = &e.Template
				s.PromptRef = &prompts.Reference{ID: e.ID, Version: e.Version, Hash: e.Hash}
				s.Prompt = ""
				s.SystemPrompt = ""
			}
			if err := walk(s.Then, depth+1); err != nil {
				return err
			}
			if err := walk(s.Else, depth+1); err != nil {
				return err
			}
			for j := range s.Branches {
				if err := walk(s.Branches[j].Steps, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(d.Steps, 0)
	if err == nil {
		for i := range d.SignalHandlers {
			if err = walk(d.SignalHandlers[i].Steps, 0); err != nil {
				break
			}
		}
	}
	return d, err
}

func validatePromptStep(s model.Step) error {
	if s.PromptTemplate == nil {
		if s.PromptRef != nil || s.PromptInputs != nil {
			return fmt.Errorf("prompt inputs/reference require a resolved template")
		}
		return nil
	}
	if s.Type != "llm" && s.Type != "decision" {
		return fmt.Errorf("prompt templates are only supported on LLM and AI Decision nodes")
	}
	if s.PromptTemplate.Variables == nil {
		s.PromptTemplate.Variables = []prompts.Variable{}
	}
	if s.Prompt != "" || s.SystemPrompt != "" {
		return fmt.Errorf("use either inline prompt fields or prompt_template, not both")
	}
	if err := prompts.Validate(*s.PromptTemplate); err != nil {
		return err
	}
	if s.PromptRef != nil && (s.PromptRef.Version < 1 || s.PromptRef.Hash != prompts.Hash(*s.PromptTemplate)) {
		return fmt.Errorf("invalid prompt snapshot hash/version")
	}
	allowed := map[string]bool{}
	for _, v := range s.PromptTemplate.Variables {
		allowed[v.Name] = true
		value, ok := s.PromptInputs[v.Name]
		if !ok && v.Required && v.Default == nil {
			return fmt.Errorf("missing mapping for prompt variable %s", v.Name)
		}
		if ok {
			text, isText := value.(string)
			if isText && referencePattern.MatchString(text) {
				continue
			}
			if !prompts.Matches(v.Type, value) {
				return fmt.Errorf("prompt variable %s expects %s", v.Name, v.Type)
			}
		}
	}
	for name := range s.PromptInputs {
		if !allowed[name] {
			return fmt.Errorf("undeclared prompt input %s", name)
		}
	}
	return nil
}

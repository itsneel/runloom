// Package prompts owns provider-independent, immutable prompt templates.
package prompts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Variable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty"`
}
type Template struct {
	Instructions string     `json:"instructions"`
	Text         string     `json:"text"`
	Variables    []Variable `json:"variables"`
}
type Reference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Hash    string `json:"hash,omitempty"`
}
type Entry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Revision    int64    `json:"revision"`
	Version     int      `json:"version"`
	Published   bool     `json:"published"`
	Template    Template `json:"template"`
	Hash        string   `json:"hash"`
	Actor       string   `json:"actor"`
	UpdatedAt   string   `json:"updated_at"`
}

var Name = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var variable = regexp.MustCompile(`\$\{vars\.([a-z][a-z0-9_-]{0,63})\}`)

// Tags describe library entries, not rendered prompt content or its hash.
func NormalizeTags(tags []string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || utf8.RuneCountInString(tag) > 40 || strings.Contains(tag, ",") || strings.IndexFunc(tag, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("tags must contain 1–40 characters without commas or control characters")
		}
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	if len(result) > 20 {
		return nil, fmt.Errorf("a prompt can have up to 20 tags")
	}
	return result, nil
}

func Hash(t Template) string { raw, _ := json.Marshal(t); return fmt.Sprintf("%x", sha256.Sum256(raw)) }
func Matches(kind string, value any) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == float64(int64(n))
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	}
	return false
}
func Validate(t Template) error {
	if strings.TrimSpace(t.Text) == "" || len(t.Text) > 16000 || len(t.Instructions) > 16000 || len(t.Variables) > 50 {
		return fmt.Errorf("prompt text is required; each text field is limited to 16000 bytes and 50 variables")
	}
	seen := map[string]bool{}
	for _, v := range t.Variables {
		if !Name.MatchString(v.Name) || seen[v.Name] {
			return fmt.Errorf("variable names must be valid and unique")
		}
		seen[v.Name] = true
		if !strings.Contains("|string|number|integer|boolean|object|array|", "|"+v.Type+"|") || v.Type == "" {
			return fmt.Errorf("unsupported variable type")
		}
		if len(v.Description) > 1000 {
			return fmt.Errorf("variable description too long")
		}
		if v.Default != nil && !Matches(v.Type, v.Default) {
			return fmt.Errorf("default for %s must be %s", v.Name, v.Type)
		}
		if !v.Required && v.Default == nil {
			return fmt.Errorf("optional variable %s requires a typed default", v.Name)
		}
	}
	for _, text := range []string{t.Instructions, t.Text} {
		for _, m := range variable.FindAllStringSubmatch(text, -1) {
			if !seen[m[1]] {
				return fmt.Errorf("undeclared prompt variable %s", m[1])
			}
		}
		if strings.Contains(variable.ReplaceAllString(text, ""), "${") {
			return fmt.Errorf("templates only support declared ${vars.name} variables; map workflow data separately")
		}
	}
	raw, _ := json.Marshal(t)
	if len(raw) > 65536 {
		return fmt.Errorf("prompt template exceeds 64 KiB")
	}
	return nil
}
func Render(t Template, inputs map[string]any) (map[string]any, error) {
	if err := Validate(t); err != nil {
		return nil, err
	}
	values := map[string]string{}
	allowed := map[string]bool{}
	for _, v := range t.Variables {
		allowed[v.Name] = true
		value, ok := inputs[v.Name]
		if !ok {
			if v.Default != nil {
				value = v.Default
			} else {
				return nil, fmt.Errorf("missing required prompt input %s", v.Name)
			}
		}
		if !Matches(v.Type, value) {
			return nil, fmt.Errorf("prompt input %s must be %s", v.Name, v.Type)
		}
		if s, ok := value.(string); ok {
			values[v.Name] = s
		} else {
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			values[v.Name] = string(raw)
		}
	}
	keys := []string{}
	for key := range inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowed[key] {
			return nil, fmt.Errorf("unknown prompt input %s", key)
		}
	}
	render := func(text string) string {
		return variable.ReplaceAllStringFunc(text, func(m string) string { return values[variable.FindStringSubmatch(m)[1]] })
	}
	system, text := render(t.Instructions), render(t.Text)
	if len(system)+len(text) > 256*1024 {
		return nil, fmt.Errorf("rendered prompt exceeds 256 KiB")
	}
	return map[string]any{"system_prompt": system, "prompt": text, "prompt_hash": Hash(t)}, nil
}

package codecontract

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

const (
	Runtime        = "quickjs-ng-0.11.0-v1"
	MaxSourceBytes = 32 * 1024
	MaxJSONBytes   = 256 * 1024
	TimeoutSeconds = 5
)

func Validate(code *model.JavaScriptCode) error {
	if code == nil || code.Runtime != Runtime {
		return fmt.Errorf("code.runtime must be %s (JavaScript only)", Runtime)
	}
	if strings.TrimSpace(code.Source) == "" || len(code.Source) > MaxSourceBytes {
		return fmt.Errorf("code.source must contain JavaScript and be at most 32 KiB")
	}
	return nil
}

func ValidateInput(input model.CodeActivityInput) error {
	if err := Validate(&input.Code); err != nil {
		return err
	}
	value, err := json.Marshal(input.Input)
	if err != nil || len(value) > MaxJSONBytes {
		return fmt.Errorf("code input must be JSON of at most 256 KiB")
	}
	return nil
}

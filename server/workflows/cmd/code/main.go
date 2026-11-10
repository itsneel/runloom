package main

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/codeexec"
	"github.com/example/runloom/workflows/internal/coderunner"
	"github.com/example/runloom/workflows/internal/model"
)

func main() {
	var input model.CodeActivityInput
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 2*codecontract.MaxJSONBytes))
	decoder.DisallowUnknownFields()
	result := codeexec.Result{Error: "invalid JavaScript request"}
	if err := decoder.Decode(&input); err == nil {
		var extra any
		if decoder.Decode(&extra) == io.EOF {
			result = coderunner.Run(context.Background(), input)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(result)
}

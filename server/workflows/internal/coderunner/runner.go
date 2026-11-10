// Package coderunner is linked only into the disposable runloom-code binary.
package coderunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/codeexec"
	"github.com/example/runloom/workflows/internal/model"
	quickjs "github.com/paralin/go-quickjs-wasi"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func Run(ctx context.Context, input model.CodeActivityInput) codeexec.Result {
	fail := func(message string) codeexec.Result { return codeexec.Result{Error: message} }
	if err := codecontract.ValidateInput(input); err != nil {
		return fail(err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, codecontract.TimeoutSeconds*time.Second)
	defer cancel()
	// No host filesystem, environment, network, entropy or wall clock capabilities.
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(1024).WithCloseOnContextDone(true))
	defer runtime.Close(context.Background())
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		return fail("could not initialize JavaScript sandbox")
	}
	payload, _ := json.Marshal(input.Input)
	if input.Input == nil {
		payload = []byte("{}")
	}
	source, _ := json.Marshal("\"use strict\";\n" + input.Code.Source)
	// JSON.parse preserves keys such as __proto__ as data, unlike a JS literal.
	inputJSON, _ := json.Marshal(string(payload))
	script := fmt.Sprintf(wrapper, string(source), string(inputJSON))
	stdout, stderr := &boundedWriter{limit: codecontract.MaxJSONBytes + 1}, &boundedWriter{limit: 2048}
	config := wazero.NewModuleConfig().WithName("javascript").
		WithArgs("qjs", "--memory-limit", "32768", "--stack-size", "512", "-e", script).
		WithStdout(stdout).WithStderr(stderr)
	_, err := runtime.InstantiateWithConfig(ctx, quickjs.QuickJSWASM, config)
	if ctx.Err() != nil {
		return fail("JavaScript exceeded its 5 second time limit or was canceled")
	}
	if stdout.overflow {
		return fail("JavaScript output exceeds 256 KiB")
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = "JavaScript failed or exceeded its memory limit"
		}
		return fail(message)
	}
	result := bytes.TrimSpace(stdout.Bytes())
	if len(result) > codecontract.MaxJSONBytes {
		return fail("JavaScript output exceeds 256 KiB")
	}
	if !json.Valid(result) {
		return fail("JavaScript must return a JSON value")
	}
	return codeexec.Result{Output: append(json.RawMessage(nil), result...)}
}

type boundedWriter struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (w *boundedWriter) Bytes() []byte  { return w.buffer.Bytes() }
func (w *boundedWriter) String() string { return w.buffer.String() }

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.buffer.Len()+len(p) > w.limit {
		w.overflow = true
		return 0, fmt.Errorf("output limit exceeded")
	}
	return w.buffer.Write(p)
}

// Function compilation keeps source literal and scoped away from the wrapper.
// This wrapper enforces the data contract; WASM capabilities, not JS globals,
// are the security boundary. Even imported QuickJS builtins have no host grants.
const wrapper = `(function () {
  const emit = globalThis.print;
  const stringify = JSON.stringify;
  const keys = Object.keys;
  const finite = Number.isFinite;
  const seen = new Set();
  function check(value, depth) {
    if (depth > 100) throw new Error("JSON output nesting exceeds 100 levels");
    if (value === null || typeof value === "string" || typeof value === "boolean") return;
    if (typeof value === "number" && finite(value)) return;
    if (typeof value !== "object" || typeof value.then === "function")
      throw new Error("Return JSON only: no undefined, functions, promises, BigInt or non-finite numbers");
    if (seen.has(value)) throw new Error("JSON output cannot contain cycles");
    seen.add(value);
    for (const key of keys(value)) check(value[key], depth + 1);
    seen.delete(value);
  }
  globalThis.print = undefined;
  globalThis.console = undefined;
  globalThis.load = undefined;
  globalThis.scriptArgs = undefined;
  const run = new Function("input", %s);
  const output = run(JSON.parse(%s));
  check(output, 0);
  const text = stringify(output);
  if (text === undefined) throw new Error("Return a JSON value");
  emit(text);
})();`

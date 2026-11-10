package coderunner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/coderunner"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func request(source string) model.CodeActivityInput {
	return model.CodeActivityInput{Code: model.JavaScriptCode{Runtime: codecontract.Runtime, Source: source}, Input: map[string]any{"name": "Runloom", "values": []int{2, 3, 4}}}
}

func TestJavaScriptJSON(t *testing.T) {
	result := coderunner.Run(context.Background(), request("return {message: `Hello ${input.name}`, total: input.values.reduce((a,b) => a+b, 0)};"))
	require.Empty(t, result.Error)
	require.JSONEq(t, `{"message":"Hello Runloom","total":9}`, string(result.Output))
	for _, source := range []string{"return null", "return [1,true,\"text\"]", "return 42", "return false", "return 'text'"} {
		t.Run(source, func(t *testing.T) {
			result := coderunner.Run(context.Background(), request(source))
			require.Empty(t, result.Error)
			require.True(t, json.Valid(result.Output))
		})
	}
}

func TestRejectedCodeAndOutput(t *testing.T) {
	for _, source := range []string{
		"const n: number = 1; return n;", "return {", "throw new Error('example error');", "return undefined;",
		"return Promise.resolve(1)", "return {nested: undefined}", "return 1n", "return NaN", "return Infinity",
		"const x = {}; x.self = x; return x", "return () => 1", "return 'x'.repeat(300000)",
		"return Array(50000000).fill('memory')", "function recurse(){ return recurse() }; return recurse();",
	} {
		t.Run(source, func(t *testing.T) {
			result := coderunner.Run(context.Background(), request(source))
			require.NotEmpty(t, result.Error)
		})
	}
}

func TestNoHostCapabilitiesOrSharedState(t *testing.T) {
	t.Setenv("RUNLOOM_TEST_SECRET", "host-secret-must-not-leak")
	result := coderunner.Run(context.Background(), request("return [typeof process, typeof require, typeof fetch, typeof XMLHttpRequest, typeof std, typeof os];"))
	require.Empty(t, result.Error)
	require.JSONEq(t, `["undefined","undefined","undefined","undefined","undefined","undefined"]`, string(result.Output))
	// The CLI's builtins cannot grant privileges absent from the WASI instance.
	result = coderunner.Run(context.Background(), request(`import('qjs:std').then(std => { if (std.getenv('RUNLOOM_TEST_SECRET') || std.open('/etc/passwd','r')) throw new Error('host access'); }); return true;`))
	require.Empty(t, result.Error)
	require.Equal(t, "true", string(result.Output))
	result = coderunner.Run(context.Background(), request("globalThis.leak = 'private'; return true;"))
	require.Empty(t, result.Error)
	result = coderunner.Run(context.Background(), request("return typeof globalThis.leak;"))
	require.Empty(t, result.Error)
	require.Equal(t, `"undefined"`, string(result.Output))
}

func TestInputPrototypeKeyIsData(t *testing.T) {
	input := request(`return {own: Object.hasOwn(input, '__proto__'), inherited: input.isAdmin === undefined};`)
	input.Input = map[string]any{"__proto__": map[string]any{"isAdmin": true}}
	result := coderunner.Run(context.Background(), input)
	require.Empty(t, result.Error)
	require.JSONEq(t, `{"own":true,"inherited":true}`, string(result.Output))
}

func TestCancellationAndInfiniteLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	result := coderunner.Run(ctx, request("while (true) {}"))
	require.NotEmpty(t, result.Error)
	require.Less(t, time.Since(start), 2*time.Second)
	result = coderunner.Run(context.Background(), request("while (true) {}"))
	require.Contains(t, result.Error, "time limit")
}

func TestLimitsBeforeExecution(t *testing.T) {
	input := request(strings.Repeat("x", codecontract.MaxSourceBytes+1))
	require.Contains(t, coderunner.Run(context.Background(), input).Error, "32 KiB")
	input = request("return input")
	input.Code.Runtime = "typescript"
	require.Contains(t, coderunner.Run(context.Background(), input).Error, "JavaScript only")
	input = request("return input")
	input.Input = map[string]any{"data": strings.Repeat("x", codecontract.MaxJSONBytes)}
	require.Contains(t, coderunner.Run(context.Background(), input).Error, "256 KiB")
}
